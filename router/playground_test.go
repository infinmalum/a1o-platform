package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/helper"
	"github.com/infinmalum/a1o-platform/controller"
	"github.com/infinmalum/a1o-platform/middleware"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type playgroundTransport func(*http.Request) (*http.Response, error)

func (transport playgroundTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPlaygroundUsesGatewayAndPrivateBilling(t *testing.T) {
	seedFixturePrices(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{},
		&model.ChatSession{}, &model.ChatMessage{}, &model.ChatFile{}, &model.ChatSkill{}, &model.ChatMCPServer{},
		&model.AgentRun{}, &model.ModelInvocation{}, &model.RunEvent{}))

	previousDB, previousLogDB, previousClient := model.DB, model.LOG_DB, client.HTTPClient
	previousSQLite, previousRedis, previousCache := common.UsingSQLite, common.RedisEnabled, config.MemoryCacheEnabled
	previousLog := config.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.RedisEnabled, config.MemoryCacheEnabled = true, false, false
	config.LogConsumeEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB, client.HTTPClient = previousDB, previousLogDB, previousClient
		common.UsingSQLite, common.RedisEnabled, config.MemoryCacheEnabled = previousSQLite, previousRedis, previousCache
		config.LogConsumeEnabled = previousLog
		_ = sqlDB.Close()
	})

	user := model.User{Username: "playground", AccessToken: "access-one", AffCode: "one", Status: model.UserStatusEnabled, Role: model.RoleCommonUser, Group: "default", Quota: 100000}
	other := model.User{Username: "other-user", AccessToken: "access-two", AffCode: "two", Status: model.UserStatusEnabled, Role: model.RoleCommonUser, Group: "default", Quota: 100000}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&other).Error)

	const prefix = "/api/chat/playground"
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestId(), sessions.Sessions("session", cookie.NewStore([]byte("playground-test-secret"))))
	engine.GET("/test/session/:username", func(c *gin.Context) {
		current := user
		if c.Param("username") == other.Username {
			current = other
		}
		session := sessions.Default(c)
		session.Set("username", current.Username)
		session.Set("id", current.Id)
		session.Set("role", current.Role)
		session.Set("status", current.Status)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	SetRelayRouter(engine)
	engine.POST("/api/chat/sessions/:id/run", middleware.UserAuth(), controller.RunChatTurn)
	engine.GET("/api/chat/sessions/:id/runs/:runId/settlement", middleware.UserAuth(), controller.GetChatTurnSettlement)

	send := func(method, path, body string, session *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if session != nil {
			request.AddCookie(session)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	missingSession := send(http.MethodPost, prefix+"/v1/responses", "{}", nil)
	require.Equal(t, http.StatusUnauthorized, missingSession.Code)
	require.Equal(t, "1", missingSession.Header().Get("X-Studio-Session-Required"))
	session := send(http.MethodGet, "/test/session/"+user.Username, "", nil).Result().Cookies()[0]
	otherSession := send(http.MethodGet, "/test/session/"+other.Username, "", nil).Result().Cookies()[0]
	missingToken := send(http.MethodPost, "/v1/responses", "{}", session)
	require.Equal(t, http.StatusUnauthorized, missingToken.Code)
	require.Empty(t, missingToken.Header().Get("X-Studio-Session-Required"))

	for _, tc := range []struct {
		model, path, upstream, body, reply string
		kind                               int
	}{
		{"chat-upstream", "/v1/chat/completions", "/chat/completions",
			`{"model":"chat-upstream","messages":[{"role":"user","content":"hello"}],"custom":42}`,
			`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4}}`, channeltype.OpenAI},
		{"responses-upstream", "/v1/responses", "/responses",
			`{"model":"responses-upstream","input":"hello","custom":42}`,
			`{"output":[],"usage":{"input_tokens":3,"output_tokens":4}}`, channeltype.OpenAIResponses},
		{"claude-upstream", "/v1/messages", "/v1/messages",
			`{"model":"claude-upstream","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"custom":42}`,
			`{"content":[],"usage":{"input_tokens":3,"output_tokens":4}}`, channeltype.Anthropic},
		{"gemini-upstream", "/v1beta/models/gemini-upstream:streamGenerateContent?alt=sse", "/v1beta/models/gemini-upstream:streamGenerateContent?alt=sse",
			`{"contents":[{"parts":[{"text":"hello"}]}],"custom":42}`,
			"data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4}}\n\n", channeltype.Gemini},
	} {
		t.Run(tc.model, func(t *testing.T) {
			baseURL := "https://upstream.example"
			channel := model.Channel{
				Type: tc.kind, Key: "upstream-key", Name: tc.model, Status: model.ChannelStatusEnabled,
				Group: "default", Models: tc.model, BaseURL: &baseURL,
			}
			require.NoError(t, channel.Insert())
			client.HTTPClient = &http.Client{Transport: playgroundTransport(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, tc.upstream, request.URL.RequestURI())
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"custom":42`)
				require.Empty(t, request.Header.Get("Cookie"))
				contentType := "application/json"
				if tc.kind == channeltype.Gemini {
					contentType = "text/event-stream"
				}
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
					Body: io.NopCloser(strings.NewReader(tc.reply)),
				}, nil
			})}

			response := send(http.MethodPost, prefix+tc.path, tc.body, session)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, tc.reply, response.Body.String())
			if tc.kind == channeltype.Gemini {
				require.Contains(t, response.Header().Get("Content-Type"), "text/event-stream")
				require.True(t, response.Flushed)
			}

			requestID := response.Header().Get(helper.RequestIdKey)
			require.NotEmpty(t, requestID)
			usage := send(http.MethodGet, prefix+"/usage/"+requestID, "", session)
			require.Equal(t, http.StatusOK, usage.Code)
			require.JSONEq(t, `{"prompt_tokens":3,"completion_tokens":4,"quota":30,"usage_state":"reported","charge_basis":"usage"}`, usage.Body.String())
			require.Equal(t, http.StatusNotFound, send(http.MethodGet, prefix+"/usage/"+requestID, "", otherSession).Code)
		})
	}
	t.Run("Gemini EOF without finish retains the reservation", func(t *testing.T) {
		before, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		frames := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4}}\n\n"
		client.HTTPClient = &http.Client{Transport: playgroundTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(frames))}, nil
		})}
		response := send(http.MethodPost, prefix+"/v1beta/models/gemini-upstream:streamGenerateContent?alt=sse",
			`{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":32}}`, session)
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, frames, response.Body.String())
		after, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		// The fixture's model ratio is 2 and its group ratio is 1.
		charged := int64(config.PreConsumedQuota+32) * 2
		require.Equal(t, charged, before-after)
		usage := send(http.MethodGet, prefix+"/usage/"+response.Header().Get(helper.RequestIdKey), "", session)
		require.Equal(t, http.StatusOK, usage.Code)
		require.JSONEq(t, fmt.Sprintf(`{"quota":%d,"prompt_tokens":3,"completion_tokens":4,"usage_state":"partial","charge_basis":"reservation","interrupted":true}`, charged), usage.Body.String())
	})
	t.Run("cancelled stream has a queryable final charge", func(t *testing.T) {
		before, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		upstream, feed := io.Pipe()
		defer feed.Close()
		client.HTTPClient = &http.Client{Transport: playgroundTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: upstream}, nil
		})}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := httptest.NewRequest(http.MethodPost, prefix+"/v1/chat/completions",
			strings.NewReader(`{"model":"chat-upstream","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":16}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(session)
		recorder := &firstWriteRecorder{ResponseRecorder: httptest.NewRecorder(), wrote: make(chan struct{}, 1)}
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			engine.ServeHTTP(recorder, req)
		}()
		go func() {
			_, _ = io.WriteString(feed, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		}()
		select {
		case <-recorder.wrote:
		case <-time.After(3 * time.Second):
			t.Fatal("stream did not write before cancellation")
		}
		requestID := recorder.Header().Get(helper.RequestIdKey)
		require.NotEmpty(t, requestID)
		cancel()
		path := prefix + "/usage/" + requestID
		require.Equal(t, http.StatusNotFound, send(http.MethodGet, path, "", session).Code)
		require.NoError(t, feed.CloseWithError(context.Canceled))
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("settlement did not finish")
		}
		after, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		require.Less(t, after, before)
		usage := send(http.MethodGet, path, "", session)
		require.Equal(t, http.StatusOK, usage.Code)
		var payload struct {
			Quota       int64  `json:"quota"`
			UsageState  string `json:"usage_state"`
			ChargeBasis string `json:"charge_basis"`
			Interrupted bool   `json:"interrupted"`
		}
		require.NoError(t, json.Unmarshal(usage.Body.Bytes(), &payload))
		require.Equal(t, before-after, payload.Quota)
		require.Equal(t, "missing", payload.UsageState)
		require.Equal(t, "reservation", payload.ChargeBasis)
		require.True(t, payload.Interrupted)
	})
	t.Run("chat cancellation changes the turn from pending to settled", func(t *testing.T) {
		chat := &model.ChatSession{UserId: user.Id, Model: "responses-upstream", Title: "test chat", MaxTokens: 16}
		require.NoError(t, model.CreateChatSession(chat))
		before, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		upstream, feed := io.Pipe()
		defer feed.Close()
		client.HTTPClient = &http.Client{Transport: playgroundTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: upstream}, nil
		})}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		base := "/api/chat/sessions/" + strconv.Itoa(chat.Id)
		req := httptest.NewRequest(http.MethodPost, base+"/run", strings.NewReader(`{"content":"hello"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(session)
		recorder := httptest.NewRecorder()
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			engine.ServeHTTP(recorder, req)
		}()
		frameRead := make(chan struct{})
		go func() {
			defer close(frameRead)
			_, _ = io.WriteString(feed, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"mock\"}}\n\n")
		}()
		select {
		case <-frameRead:
		case <-time.After(3 * time.Second):
			t.Fatal("Chat did not begin reading the upstream")
		}
		runID := recorder.Header().Get("X-A1O-Run-Id")
		require.NotEmpty(t, runID)
		path := base + "/runs/" + runID + "/settlement"
		require.Equal(t, http.StatusAccepted, send(http.MethodGet, path, "", session).Code)
		require.Equal(t, http.StatusNotFound, send(http.MethodGet, path, "", otherSession).Code)
		cancel()
		require.NoError(t, feed.CloseWithError(context.Canceled))
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("Chat did not finish after cancellation")
		}
		after, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		require.Less(t, after, before)
		usage := send(http.MethodGet, path, "", session)
		require.Equal(t, http.StatusOK, usage.Code)
		require.JSONEq(t, fmt.Sprintf(`{"quota":%d,"prompt_tokens":0,"completion_tokens":0,"usage_state":"missing","charge_basis":"reservation","interrupted":true}`, before-after), usage.Body.String())
	})
	t.Run("studio audio accepts bounded multipart without exposing billing credentials", func(t *testing.T) {
		baseURL := "https://upstream.example"
		channel := model.Channel{
			Type: channeltype.OpenAI, Key: "audio-provider-key", Name: "audio", Status: model.ChannelStatusEnabled,
			Group: "default", Models: "whisper-1", BaseURL: &baseURL,
		}
		require.NoError(t, channel.Insert())
		client.HTTPClient = &http.Client{Transport: playgroundTransport(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/audio/transcriptions", request.URL.Path)
			require.Equal(t, "Bearer audio-provider-key", request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("Cookie"))
			require.Contains(t, request.Header.Get("Content-Type"), "multipart/form-data; boundary=")
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "audio sample")
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"text":"sample transcript"}`))}, nil
		})}
		var form bytes.Buffer
		writer := multipart.NewWriter(&form)
		require.NoError(t, writer.WriteField("model", "whisper-1"))
		file, err := writer.CreateFormFile("file", "sample.wav")
		require.NoError(t, err)
		_, err = io.WriteString(file, "audio sample")
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		for _, origin := range []string{"same-origin", "cross-site"} {
			request := httptest.NewRequest(http.MethodPost, "/api/studio/media/v1/audio/transcriptions", bytes.NewReader(form.Bytes()))
			request.Header.Set("Content-Type", writer.FormDataContentType())
			request.Header.Set("Sec-Fetch-Site", origin)
			request.AddCookie(session)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if origin == "cross-site" {
				require.Equal(t, http.StatusForbidden, response.Code)
				continue
			}
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.JSONEq(t, `{"text":"sample transcript"}`, response.Body.String())
		}
	})

}
