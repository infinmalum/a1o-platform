package router

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/helper"
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
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}))

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
			"data: {\"candidates\":[],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4}}\n\n", channeltype.Gemini},
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
			require.JSONEq(t, `{"prompt_tokens":3,"completion_tokens":4,"quota":30}`, usage.Body.String())
			require.Equal(t, http.StatusNotFound, send(http.MethodGet, prefix+"/usage/"+requestID, "", otherSession).Code)
		})
	}
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
