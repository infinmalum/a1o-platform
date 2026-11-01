package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// TestIntegrationToolLoopBillsThroughLifecycle drives the engine with the
// real ModelExecutor against a scripted Responses upstream: leg one requests
// a tool call, leg two answers with text after seeing the tool result. Every
// leg settles through the normal lifecycle and lands on the chat token.
func TestIntegrationToolLoopBillsThroughLifecycle(t *testing.T) {
	var bodies []string
	upstream := newResponsesUpstream(t, &bodies)
	defer upstream.Close()
	db := setupExecutorDB(t)
	seedExecutorPrices(t)

	user := &model.User{Username: "chat-user", Status: model.UserStatusEnabled, Quota: 1000000, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	token, err := model.EnsureChatToken(user.Id)
	if err != nil {
		t.Fatalf("EnsureChatToken: %v", err)
	}

	baseURL := upstream.URL
	channel := model.Channel{
		Name: "chat-upstream", Type: channeltype.OpenAIResponses, Key: "sk-channel", Status: model.ChannelStatusEnabled,
		Group: "default", Models: "agent-model", BaseURL: &baseURL,
	}
	if err := channel.Insert(); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	session := &model.ChatSession{UserId: user.Id, Model: "agent-model"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "算一下 1+1"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)
	history, err := model.GetSessionMessages(session.Id)
	if err != nil {
		t.Fatal(err)
	}
	run := &model.AgentRun{UserId: user.Id, SessionId: session.Id, Kind: "chat",
		Status: model.RunStatusQueued, Model: "agent-model"}
	if err := model.CreateAgentRun(run); err != nil {
		t.Fatal(err)
	}

	tools := newToolSet()
	tools.add(Tool{Name: "echo"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "echo: 2"}
	})
	engine := &Engine{extraTools: tools}
	executor := &ModelExecutor{UserID: user.Id, TokenID: token.Id, TokenName: token.Name}

	var events []EventEnvelope
	err = engine.RunChat(context.Background(), collectEvents(&events), ChatTurnRequest{
		Run:           run,
		Session:       session,
		History:       history,
		Executor:      executor,
		ContextBudget: 1000000,
	})
	if err != nil {
		t.Fatalf("RunChat failed: %v", err)
	}

	// The model saw the tool result in the second leg.
	if len(bodies) != 2 {
		t.Fatalf("upstream hits = %d, want 2", len(bodies))
	}
	for _, want := range []string{`"type":"function_call"`, `"call_1"`, `"echo: 2"`, `"type":"function_call_output"`} {
		if !strings.Contains(bodies[1], want) {
			t.Fatalf("second leg missing %s: %s", want, bodies[1])
		}
	}

	// Settlement produced one consume log per leg, on the chat token.
	var logs []model.Log
	if err := db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("consume logs = %d, want 2", len(logs))
	}
	for _, entry := range logs {
		if entry.TokenName != token.Name || entry.ChannelId != channel.Id {
			t.Fatalf("log misattributed: %+v", entry)
		}
		if !entry.IsStream {
			t.Fatalf("leg should settle as stream: %+v", entry)
		}
	}
	if logs[0].PromptTokens != 12 || logs[0].CompletionTokens != 5 {
		t.Fatalf("first leg usage = %d/%d", logs[0].PromptTokens, logs[0].CompletionTokens)
	}
	if logs[1].PromptTokens != 20 || logs[1].CompletionTokens != 3 {
		t.Fatalf("second leg usage = %d/%d", logs[1].PromptTokens, logs[1].CompletionTokens)
	}
	// Each invocation records its settlement association.
	invocations, err := model.GetRunInvocations(run.Id)
	if err != nil || len(invocations) != 2 {
		t.Fatalf("invocations = %d, err %v", len(invocations), err)
	}
	for i, invocation := range invocations {
		if invocation.ConsumeLogId == 0 || invocation.ConsumeLogId != logs[i].Id {
			t.Fatalf("invocation %d log association broken: %d vs %d", i, invocation.ConsumeLogId, logs[i].Id)
		}
	}

	// Rows persisted: user, assistant(tool_calls, reasoning), tool, assistant.
	messages, err := model.GetSessionMessages(session.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(messages))
	}
	if messages[1].ToolCalls == nil || messages[2].Role != model.ChatRoleTool {
		t.Fatalf("tool round trip rows wrong: %+v %+v", messages[1], messages[2])
	}
	if messages[1].Reasoning == nil || !strings.Contains(*messages[1].Reasoning, "enc-sealed-blob") {
		t.Fatalf("assistant reasoning state not persisted: %v", messages[1].Reasoning)
	}

	// User quota was charged by the normal settlement path. (The native
	// settlement path maintains user and channel counters; the unlimited
	// chat token's own used-quota column is not part of it.)
	var stored model.User
	if err := db.First(&stored, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Quota >= user.Quota {
		t.Fatalf("user quota not charged: %d", stored.Quota)
	}
	// Structural events reached the run's event log.
	storedEvents, err := model.GetRunEventsAfter(run.Id, 0)
	if err != nil || len(storedEvents) == 0 {
		t.Fatalf("run events = %d, err %v", len(storedEvents), err)
	}
}

func TestEnsureChatTokenCreatesOnceAndReactivates(t *testing.T) {
	db := setupExecutorDB(t)
	user := &model.User{Username: "token-user", Status: model.UserStatusEnabled, Quota: 1000, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	first, err := model.EnsureChatToken(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != model.ChatTokenName || !first.UnlimitedQuota || first.ExpiredTime != -1 {
		t.Fatalf("token shape wrong: %+v", first)
	}
	second, err := model.EnsureChatToken(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	if second.Id != first.Id {
		t.Fatalf("second lookup created a new token: %d vs %d", second.Id, first.Id)
	}
	if err := db.Model(&model.Token{}).Where("id = ?", first.Id).
		Update("status", model.TokenStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	restored, err := model.EnsureChatToken(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != model.TokenStatusEnabled {
		t.Fatalf("disabled chat token not reactivated: %d", restored.Status)
	}
}
