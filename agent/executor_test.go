package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// setupExecutorDB mirrors the chat integration fixture: real channel selection
// and settlement against in-memory SQLite with caches disabled, plus the agent
// runtime tables.
func setupExecutorDB(t *testing.T) *gorm.DB {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open("file:agent-exec-"+hex.EncodeToString(buf)+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.UpstreamAccount{}, &model.UpstreamAccountCooldown{}, &model.Log{},
		&model.AgentRun{}, &model.ModelInvocation{}, &model.RunEvent{}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemoryCache, previousSQLite := common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite
	previousLogConsume := config.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = false, false, true
	config.LogConsumeEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = previousRedis, previousMemoryCache, previousSQLite
		config.LogConsumeEnabled = previousLogConsume
		_ = sqlDB.Close()
	})
	return db
}

// seedExecutorPrices prices the fixture model; the shipped price book is
// empty and a model bills only what an operator priced.
func seedExecutorPrices(t *testing.T) {
	t.Helper()
	previousInput, previousCompletion := billingratio.ModelRatio2JSONString(), billingratio.CompletionRatio2JSONString()
	t.Cleanup(func() {
		_ = billingratio.UpdateModelRatioByJSONString(previousInput)
		_ = billingratio.UpdateCompletionRatioByJSONString(previousCompletion)
	})
	if err := billingratio.UpdateModelRatioByJSONString(`{"agent-model": 2}`); err != nil {
		t.Fatal(err)
	}
	if err := billingratio.UpdateCompletionRatioByJSONString(`{"agent-model": 3}`); err != nil {
		t.Fatal(err)
	}
}

// newExecutorFixture wires user, token, channel, and a run for one upstream
// kind, then returns the executor and the run.
func newExecutorFixture(t *testing.T, channelType int, upstream *httptest.Server) (*ModelExecutor, *model.AgentRun) {
	t.Helper()
	db := setupExecutorDB(t)
	seedExecutorPrices(t)
	user := &model.User{Username: fmt.Sprintf("agent-user-%d", channelType), Status: model.UserStatusEnabled, Quota: 1000000, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	token, err := model.EnsureChatToken(user.Id)
	if err != nil {
		t.Fatalf("EnsureChatToken: %v", err)
	}
	baseURL := upstream.URL
	channel := model.Channel{
		Name: fmt.Sprintf("agent-upstream-%d", channelType), Type: channelType, Key: "sk-channel",
		Status: model.ChannelStatusEnabled, Group: "default", Models: "agent-model", BaseURL: &baseURL,
	}
	if err := channel.Insert(); err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	executor := &ModelExecutor{UserID: user.Id, TokenID: token.Id, TokenName: token.Name}
	run := &model.AgentRun{UserId: user.Id, Kind: "chat", Status: model.RunStatusRunning,
		IdempotencyKey: fmt.Sprintf("fixture-%d", channelType), Model: "agent-model", Instructions: "你是测试助手"}
	if err := model.CreateAgentRun(run); err != nil {
		t.Fatal(err)
	}
	return executor, run
}

func userTurn(text string) []Item {
	return []Item{{Type: ItemMessage, Role: "user", Content: []ContentPart{{Type: PartInputText, Text: text}}}}
}

func echoTool() []Tool {
	return []Tool{{Name: "echo", Description: "回声", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`)}}
}

// runContextItems reads the run's persisted context back as standard Items.
func runContextItems(t *testing.T, run *model.AgentRun) []Item {
	t.Helper()
	stored, err := model.GetAgentRun(run.Id, run.UserId)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if stored.ContextItems == nil || *stored.ContextItems == "" {
		return nil
	}
	var items []Item
	if err := json.Unmarshal([]byte(*stored.ContextItems), &items); err != nil {
		t.Fatalf("run context is not standard Items: %v", err)
	}
	return items
}

func loadInvocation(t *testing.T, runID int) model.ModelInvocation {
	t.Helper()
	invocations, err := model.GetRunInvocations(runID)
	if err != nil || len(invocations) == 0 {
		t.Fatalf("invocations = %d, err %v", len(invocations), err)
	}
	return invocations[len(invocations)-1]
}

func loadConsumeLog(t *testing.T, tokenName string) model.Log {
	t.Helper()
	var log model.Log
	if err := model.DB.Where("token_name = ? AND type = ?", tokenName, model.LogTypeConsume).
		Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("consume log missing: %v", err)
	}
	return log
}

// newResponsesUpstream scripts an OpenAI Responses upstream: leg one returns a
// reasoning item with sealed thinking plus a function call, leg two answers
// with text.
func newResponsesUpstream(t *testing.T, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(payload))
		hit := len(*bodies)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if hit == 1 {
			_, _ = fmt.Fprint(w,
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"agent-model\"}}\n\n"+
					"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rsn_1\",\"summary\":[]}}\n\n"+
					"data: {\"type\":\"response.reasoning_summary_text.delta\",\"item_id\":\"rsn_1\",\"output_index\":0,\"summary_index\":0,\"delta\":\"盘算\"}\n\n"+
					"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rsn_1\",\"status\":\"completed\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"盘算一下\"}],\"encrypted_content\":\"enc-sealed-blob\"}}\n\n"+
					"data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"status\":\"in_progress\",\"call_id\":\"call_1\",\"name\":\"echo\",\"arguments\":\"\"}}\n\n"+
					"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc_1\",\"output_index\":1,\"delta\":\"{\\\"x\\\":1}\"}\n\n"+
					"data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"status\":\"completed\",\"call_id\":\"call_1\",\"name\":\"echo\",\"arguments\":\"{\\\"x\\\":1}\"}}\n\n"+
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"agent-model\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rsn_1\",\"status\":\"completed\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"盘算一下\"}],\"encrypted_content\":\"enc-sealed-blob\"},{\"type\":\"function_call\",\"id\":\"fc_1\",\"status\":\"completed\",\"call_id\":\"call_1\",\"name\":\"echo\",\"arguments\":\"{\\\"x\\\":1}\"}],\"usage\":{\"input_tokens\":12,\"output_tokens\":5,\"input_tokens_details\":{\"cached_tokens\":4},\"output_tokens_details\":{\"reasoning_tokens\":2}}}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w,
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_2\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"agent-model\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"等于 2\",\"annotations\":[]}]}],\"usage\":{\"input_tokens\":20,\"output_tokens\":3}}}\n\n")
	}))
}

// newAnthropicUpstream scripts a Messages upstream: leg one emits thinking
// with a signature plus a tool_use block, leg two answers with text.
func newAnthropicUpstream(t *testing.T, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(payload))
		hit := len(*bodies)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if hit == 1 {
			_, _ = fmt.Fprint(w,
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ant_1\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":12,\"output_tokens\":1}}}\n\n"+
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"推理中\"}}\n\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-abc\"}}\n\n"+
					"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
					"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_ant\",\"name\":\"echo\",\"input\":{}}}\n\n"+
					"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"x\\\":1}\"}}\n\n"+
					"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n"+
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":7}}\n\n"+
					"data: {\"type\":\"message_stop\"}\n\n")
			return
		}
		_, _ = fmt.Fprint(w,
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ant_2\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":30,\"output_tokens\":1}}}\n\n"+
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"等于 2\"}}\n\n"+
				"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n")
	}))
}

// newGeminiUpstream scripts a GenerateContent upstream: leg one emits a signed
// thought part plus a function call, leg two answers with text.
func newGeminiUpstream(t *testing.T, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(payload))
		hit := len(*bodies)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if hit == 1 {
			_, _ = fmt.Fprint(w,
				"data: {\"responseId\":\"gem-1\",\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"想一想\",\"thought\":true,\"thoughtSignature\":\"tm-sig\"}]}}]}\n\n"+
					"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"echo\",\"args\":{\"x\":1}}}]}}]}\n\n"+
					"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"好了\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":7,\"thoughtsTokenCount\":3}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w,
			"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"等于 2\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":25,\"candidatesTokenCount\":4}}\n\n")
	}))
}

// newChatUpstream scripts a Chat Completions upstream: leg one requests a tool
// call, leg two answers with text.
func newChatUpstream(t *testing.T, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(payload))
		hit := len(*bodies)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if hit == 1 {
			_, _ = fmt.Fprint(w,
				"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n"+
					"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_chat\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"x\\\":1}\"}}]}}]}\n\n"+
					"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"+
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":5}}\n\n"+
					"data: [DONE]\n\n")
			return
		}
		_, _ = fmt.Fprint(w,
			"data: {\"choices\":[{\"delta\":{\"content\":\"等于 2\"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":3}}\n\n"+
				"data: [DONE]\n\n")
	}))
}

// invokeLegOne runs the scripted tool-call leg and returns the response items.
func invokeLegOne(t *testing.T, executor *ModelExecutor, run *model.AgentRun) InvokeResult {
	t.Helper()
	result, err := executor.Invoke(context.Background(), InvokeRequest{
		RunID: run.Id, Model: "agent-model", Instructions: "你是测试助手",
		Items: userTurn("算一下 1+1"), Tools: echoTool(), MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatalf("leg one failed: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("leg one status = %q, want completed", result.Status)
	}
	return result
}

// continueWithToolOutput replays the persisted context plus the tool result
// and returns the second leg's result.
func continueWithToolOutput(t *testing.T, executor *ModelExecutor, run *model.AgentRun, callID string) InvokeResult {
	t.Helper()
	items := runContextItems(t, run)
	items = append(items, Item{Type: ItemFunctionCallOutput, CallID: callID, Output: "echo: 2"})
	result, err := executor.Invoke(context.Background(), InvokeRequest{
		RunID: run.Id, Model: "agent-model", Instructions: "你是测试助手",
		Items: items, Tools: echoTool(), MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatalf("leg two failed: %v", err)
	}
	return result
}

func assertBilledOnce(t *testing.T, executor *ModelExecutor, runID int) model.Log {
	t.Helper()
	invocation := loadInvocation(t, runID)
	if invocation.Status != model.InvocationStatusSucceeded {
		t.Fatalf("invocation status = %q, want succeeded (%s)", invocation.Status, invocation.Error)
	}
	if invocation.FinishStatus != "completed" {
		t.Fatalf("invocation finish status = %q", invocation.FinishStatus)
	}
	log := loadConsumeLog(t, executor.TokenName)
	if invocation.ConsumeLogId == 0 || invocation.ConsumeLogId != log.Id {
		t.Fatalf("invocation log id %d does not match consume log %d", invocation.ConsumeLogId, log.Id)
	}
	if log.Quota <= 0 {
		t.Fatalf("consume log charged nothing: %+v", log)
	}
	if invocation.Usage == nil || !strings.Contains(*invocation.Usage, `"Seen":true`) {
		t.Fatalf("invocation usage snapshot missing: %v", invocation.Usage)
	}
	events, err := model.GetRunEventsAfter(runID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var completed int
	for _, event := range events {
		if event.Type == model.RunEventInvocationCompleted {
			completed++
		}
	}
	if completed == 0 {
		t.Fatalf("no invocation.completed event among %d events", len(events))
	}
	return log
}

func TestInvokeResponsesUpstreamRoundTripsEncryptedReasoning(t *testing.T) {
	var bodies []string
	upstream := newResponsesUpstream(t, &bodies)
	defer upstream.Close()
	executor, run := newExecutorFixture(t, channeltype.OpenAIResponses, upstream)

	result := invokeLegOne(t, executor, run)
	if len(result.Items) != 2 {
		t.Fatalf("leg one items = %d, want reasoning + function_call", len(result.Items))
	}
	if result.Items[0].EncryptedContent != "enc-sealed-blob" || result.Items[0].Summary[0].Text != "盘算一下" {
		t.Fatalf("sealed reasoning lost: %+v", result.Items[0])
	}
	if result.Items[1].CallID != "call_1" || result.Items[1].Arguments != `{"x":1}` {
		t.Fatalf("function call lost: %+v", result.Items[1])
	}
	if result.Usage.Input != 12 || result.Usage.Output != 5 || result.Usage.CacheRead != 4 || result.Usage.Reasoning != 2 {
		t.Fatalf("usage details lost: %+v", result.Usage)
	}

	items := runContextItems(t, run)
	if len(items) != 2 || items[0].EncryptedContent != "enc-sealed-blob" {
		t.Fatalf("persisted context lost the sealed reasoning: %+v", items)
	}
	assertBilledOnce(t, executor, run.Id)

	continueWithToolOutput(t, executor, run, "call_1")
	if len(bodies) != 2 {
		t.Fatalf("upstream hits = %d, want 2", len(bodies))
	}
	var replayed struct {
		Store        *bool             `json:"store"`
		Include      []string          `json:"include"`
		Instructions string            `json:"instructions"`
		Input        []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.Store == nil || *replayed.Store {
		t.Fatalf("store flag missing on replayed request: %v", replayed.Store)
	}
	if len(replayed.Include) != 1 || replayed.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("encrypted reasoning include missing: %v", replayed.Include)
	}
	if replayed.Instructions != "你是测试助手" {
		t.Fatalf("instructions lost: %q", replayed.Instructions)
	}
	joined := bodies[1]
	for _, want := range []string{
		`"type":"reasoning"`, `"encrypted_content":"enc-sealed-blob"`,
		`"call_id":"call_1"`, `"type":"function_call_output"`, `"echo: 2"`,
		`"type":"function_call"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("replayed request lost %s: %s", want, joined)
		}
	}
	assertBilledOnce(t, executor, run.Id)
}

func TestInvokeAnthropicUpstreamRoundTripsThinkingSignature(t *testing.T) {
	var bodies []string
	upstream := newAnthropicUpstream(t, &bodies)
	defer upstream.Close()
	executor, run := newExecutorFixture(t, channeltype.Anthropic, upstream)

	result := invokeLegOne(t, executor, run)
	if len(result.Items) != 2 {
		t.Fatalf("leg one items = %d, want reasoning + function_call", len(result.Items))
	}
	if result.Items[0].Signature != "sig-abc" || result.Items[0].Summary[0].Text != "推理中" {
		t.Fatalf("thinking signature lost: %+v", result.Items[0])
	}
	if result.Items[1].CallID != "call_ant" {
		t.Fatalf("tool call lost: %+v", result.Items[1])
	}
	assertBilledOnce(t, executor, run.Id)

	continueWithToolOutput(t, executor, run, "call_ant")
	if len(bodies) != 2 {
		t.Fatalf("upstream hits = %d, want 2", len(bodies))
	}
	joined := bodies[1]
	for _, want := range []string{
		`"type":"thinking"`, `"thinking":"推理中"`, `"signature":"sig-abc"`,
		`"type":"tool_use"`, `"tool_use_id":"call_ant"`, `"echo: 2"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("replayed Messages request lost %s: %s", want, joined)
		}
	}
	assertBilledOnce(t, executor, run.Id)
}

func TestInvokeGeminiUpstreamRoundTripsThoughtSignature(t *testing.T) {
	var bodies []string
	upstream := newGeminiUpstream(t, &bodies)
	defer upstream.Close()
	executor, run := newExecutorFixture(t, channeltype.Gemini, upstream)

	result := invokeLegOne(t, executor, run)
	if len(result.Items) != 3 {
		t.Fatalf("leg one items = %d, want reasoning + function_call + message", len(result.Items))
	}
	if result.Items[0].Summary[0].Text != "想一想" || result.Items[0].Signature != "tm-sig" {
		t.Fatalf("thought part lost: %+v", result.Items[0])
	}
	if result.Items[1].Name != "echo" || result.Items[1].Arguments != `{"x":1}` {
		t.Fatalf("function call lost: %+v", result.Items[1])
	}
	assertBilledOnce(t, executor, run.Id)

	continueWithToolOutput(t, executor, run, result.Items[1].CallID)
	if len(bodies) != 2 {
		t.Fatalf("upstream hits = %d, want 2", len(bodies))
	}
	joined := bodies[1]
	for _, want := range []string{
		`"thought":true`, `"thoughtSignature":"tm-sig"`, `"functionCall"`, `"functionResponse"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("replayed GenerateContent request lost %s: %s", want, joined)
		}
	}
	assertBilledOnce(t, executor, run.Id)
}

// TestInvokeOpenResponsesPassthrough proves the OpenResponses candidate is
// reachable: a Vercel gateway channel receives the same body the Responses
// upstream would, since bridge.For keeps the two protocol identities on the
// shared item conversion and the wire passes through.
func TestInvokeOpenResponsesPassthrough(t *testing.T) {
	var bodies []string
	upstream := newResponsesUpstream(t, &bodies)
	defer upstream.Close()
	executor, run := newExecutorFixture(t, channeltype.Vercel, upstream)

	result := invokeLegOne(t, executor, run)
	if len(result.Items) != 2 || result.Items[0].EncryptedContent != "enc-sealed-blob" {
		t.Fatalf("OpenResponses passthrough lost items: %+v", result.Items)
	}
	continueWithToolOutput(t, executor, run, "call_1")
	if len(bodies) != 2 {
		t.Fatalf("upstream hits = %d, want 2", len(bodies))
	}
	for _, want := range []string{
		`"encrypted_content":"enc-sealed-blob"`, `"type":"function_call_output"`, `"store":false`,
	} {
		if !strings.Contains(bodies[1], want) {
			t.Fatalf("replayed OpenResponses request lost %s: %s", want, bodies[1])
		}
	}
	assertBilledOnce(t, executor, run.Id)
}

func TestInvokeWithoutChannelFailsCleanly(t *testing.T) {
	db := setupExecutorDB(t)
	seedExecutorPrices(t)
	user := &model.User{Username: "agent-user-none", Status: model.UserStatusEnabled, Quota: 1000000, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	token, err := model.EnsureChatToken(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	executor := &ModelExecutor{UserID: user.Id, TokenID: token.Id, TokenName: token.Name}
	run := &model.AgentRun{UserId: user.Id, Kind: "chat", Status: model.RunStatusRunning, Model: "agent-model"}
	if err := model.CreateAgentRun(run); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Invoke(context.Background(), InvokeRequest{
		RunID: run.Id, Model: "agent-model", Items: userTurn("hi"),
	})
	if err == nil || !strings.Contains(err.Error(), "no compatible channel") {
		t.Fatalf("expected channel failure, got %v", err)
	}
	// No invocation was created because selection happens before the intent.
	invocations, err := model.GetRunInvocations(run.Id)
	if err != nil || len(invocations) != 0 {
		t.Fatalf("failed selection persisted %d invocations", len(invocations))
	}
}

func TestBuildResponsesRequestWireShapes(t *testing.T) {
	items := []Item{
		{Type: ItemMessage, Role: "user", Content: []ContentPart{{Type: PartInputText, Text: "hi"}}},
		{Type: ItemReasoning, Summary: []SummaryPart{{Type: PartSummaryText, Text: "想"}},
			Signature: "s1"},
		{Type: ItemFunctionCall, CallID: "c1", Name: "echo", Arguments: "{}", ThoughtSignature: "t1"},
		{Type: ItemFunctionCallOutput, CallID: "c1", Output: "ok"},
	}
	body, err := buildResponsesRequest(InvokeRequest{Model: "m", Items: items,
		Instructions: "sys", Tools: echoTool()}, native.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	joined := string(body)
	if strings.Contains(joined, `"store"`) || strings.Contains(joined, `"include"`) {
		t.Fatalf("bridged body must omit Responses-only fields: %s", joined)
	}
	if !strings.Contains(joined, `"signature":"s1"`) || !strings.Contains(joined, `"thought_signature":"t1"`) {
		t.Fatalf("opaque state slots lost: %s", joined)
	}

	responsesBody, err := buildResponsesRequest(InvokeRequest{Model: "m", Items: items}, native.OpenAIResponses)
	if err != nil {
		t.Fatal(err)
	}
	var shaped struct {
		Store   *bool    `json:"store"`
		Include []string `json:"include"`
	}
	if err := json.Unmarshal(responsesBody, &shaped); err != nil {
		t.Fatal(err)
	}
	if shaped.Store == nil || *shaped.Store || len(shaped.Include) != 1 {
		t.Fatalf("Responses body lost store/include: %s", responsesBody)
	}

	if _, err := buildResponsesRequest(InvokeRequest{Model: "m", Items: []Item{{Type: "web_search_call"}}}, native.Anthropic); err == nil {
		t.Fatal("unknown item type must fail loudly")
	}
	if _, err := buildResponsesRequest(InvokeRequest{Model: "m", Items: []Item{{Type: ItemMessage, Role: "system",
		Content: []ContentPart{{Type: PartInputText, Text: "x"}}}}}, native.Anthropic); err == nil {
		t.Fatal("system role must fail loudly on the agent leg")
	}
}
