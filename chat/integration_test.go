package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/one-gateway/common"
	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
)

// setupIntegrationDB mirrors the lifecycle fixtures: real channel selection
// and settlement against in-memory SQLite with caches disabled.
func setupIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open("file:chat-int-"+hex.EncodeToString(buf)+"?mode=memory&cache=shared"), &gorm.Config{})
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
		&model.ChatSession{}, &model.ChatMessage{}, &model.ChatFile{},
		&model.ChatSkill{}, &model.ChatMCPServer{}); err != nil {
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

// newChatUpstream serves scripted chat completions legs: leg one requests a
// tool call, leg two answers with text after seeing the tool result.
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
					"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_x\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"value\\\":\\\"1+1\\\"}\"}}]}}]}\n\n"+
					"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"+
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4}}\n\n"+
					"data: [DONE]\n\n")
			return
		}
		_, _ = fmt.Fprint(w,
			"data: {\"choices\":[{\"delta\":{\"content\":\"等于 2。\"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":25,\"completion_tokens\":6}}\n\n"+
				"data: [DONE]\n\n")
	}))
}

func TestIntegrationToolLoopBillsThroughLifecycle(t *testing.T) {
	db := setupIntegrationDB(t)
	var bodies []string
	upstream := newChatUpstream(t, &bodies)
	defer upstream.Close()

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
		Name: "chat-upstream", Type: 1, Key: "sk-channel", Status: model.ChannelStatusEnabled,
		Group: "default", Models: "test-model", BaseURL: &baseURL,
	}
	if err := channel.Insert(); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	session := &model.ChatSession{UserId: user.Id, Model: "test-model"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "算一下 1+1"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)

	tools := newToolSet()
	tools.add(Tool{Name: "echo"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "echo: 2"}
	})

	var events []Event
	runtime := &Runtime{}
	err = runtime.runLoop(context.Background(), collectEvents(&events), TurnRequest{
		Session: session,
		History: userMessageAsHistory(userMessage),
		Leg:     NewLifecycleLeg("test-model", user.Id, token.Id, token.Name),
	}, tools)
	if err != nil {
		t.Fatalf("runLoop failed: %v", err)
	}

	// The model saw the tool result in the second leg.
	if len(bodies) != 2 {
		t.Fatalf("upstream hits = %d, want 2", len(bodies))
	}
	if !strings.Contains(bodies[1], `"role":"tool"`) || !strings.Contains(bodies[1], "echo: 2") {
		t.Fatalf("second leg missing tool result: %s", bodies[1])
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
		if entry.TokenName != model.ChatTokenName || entry.ChannelId != channel.Id {
			t.Fatalf("log misattributed: %+v", entry)
		}
		if !entry.IsStream {
			t.Fatalf("leg should settle as stream: %+v", entry)
		}
	}
	if logs[0].PromptTokens != 10 || logs[0].CompletionTokens != 4 {
		t.Fatalf("first leg usage = %d/%d", logs[0].PromptTokens, logs[0].CompletionTokens)
	}
	if logs[1].PromptTokens != 25 || logs[1].CompletionTokens != 6 {
		t.Fatalf("second leg usage = %d/%d", logs[1].PromptTokens, logs[1].CompletionTokens)
	}

	// Rows persisted: user, assistant(tool_calls), tool, assistant.
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
}

func TestEnsureChatTokenCreatesOnceAndReactivates(t *testing.T) {
	db := setupIntegrationDB(t)
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
