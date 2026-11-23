package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/blacklist"
	"github.com/infinmalum/a1o-platform/middleware"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPrivacyReviewRoutesAndSessionRevocation(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	before, redis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = before, redis; _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.ChatSession{}, &model.ChatMessage{}, &model.PrivacyReviewAccess{},
		&model.AgentRun{}, &model.Token{}, &model.ImageGeneration{}))
	root := model.User{Username: "review-root", AccessToken: "root-review-key", AffCode: "root-review", Role: model.RoleRootUser}
	user := model.User{Username: "review-owner", AccessToken: "owner-review-key", AffCode: "owner-review", Role: model.RoleAdminUser}
	require.NoError(t, db.Create(&root).Error)
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.Token{UserId: user.Id, Key: "fixture-relay-key", Status: model.TokenStatusEnabled,
		ExpiredTime: -1, UnlimitedQuota: true}).Error)
	t.Cleanup(func() { blacklist.UnbanUser(user.Id) })
	active := model.ChatSession{UserId: user.Id, Title: "visible chat"}
	retained := model.ChatSession{UserId: user.Id, Title: "retained chat"}
	require.NoError(t, db.Create(&active).Error)
	require.NoError(t, db.Create(&retained).Error)
	require.NoError(t, model.InsertChatMessage(&model.ChatMessage{SessionId: retained.Id, Role: model.ChatRoleUser, Content: "retained message"}))
	require.NoError(t, model.DeleteChatSession(retained.Id, user.Id))

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("fixture-privacy-cookie"))))
	engine.GET("/test-login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	engine.GET("/test-token", middleware.TokenAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	SetApiRouter(engine)
	send := func(method, path, token, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	route := fmt.Sprintf("/api/privacy/review/chats/%d", retained.Id)
	rootToken := root.AccessToken
	require.NotContains(t, send(http.MethodPost, route, user.AccessToken, `{"reason":"case"}`, nil).Body.String(), "retained message")
	require.Equal(t, http.StatusBadRequest, send(http.MethodPost, route, rootToken, `{"reason":""}`, nil).Code)
	require.Contains(t, send(http.MethodPost, route, rootToken, `{"reason":"case"}`, nil).Body.String(), "retained message")
	require.Equal(t, http.StatusNotFound, send(http.MethodPost, fmt.Sprintf("/api/privacy/review/chats/%d", active.Id), rootToken, `{"reason":"case"}`, nil).Code)
	require.NoError(t, db.Model(&retained).Update("privacy_purge_at", time.Now().Unix()-1).Error)
	require.Equal(t, http.StatusNotFound, send(http.MethodPost, route, rootToken, `{"reason":"case"}`, nil).Code)
	var audit int64
	require.NoError(t, db.Model(&model.PrivacyReviewAccess{}).Count(&audit).Error)
	require.EqualValues(t, 1, audit)

	require.Equal(t, http.StatusNoContent, send(http.MethodGet, "/test-token", "sk-fixture-relay-key", "", nil).Code)
	login := send(http.MethodGet, "/test-login", "", "", nil)
	require.Len(t, login.Result().Cookies(), 1)
	stale := login.Result().Cookies()[0]
	require.Contains(t, send(http.MethodDelete, "/api/user/self", "", "", stale).Body.String(), `"success":true`)
	blacklist.UnbanUser(user.Id) // A different replica would not have the local flag.
	require.NotEqual(t, http.StatusNoContent, send(http.MethodGet, "/test-token", "sk-fixture-relay-key", "", nil).Code)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/user/privacy/memory", "", "", stale).Code)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/user/privacy/memory", user.AccessToken, "", nil).Code)
}
