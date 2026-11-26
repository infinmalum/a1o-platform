package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStudioModelCatalog(t *testing.T) {
	seedFixturePrices(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Ability{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = previousDB, previousRedis })

	user := model.User{Username: "catalog", AccessToken: "catalog-token", AffCode: "catalog", Status: model.UserStatusEnabled, Role: model.RoleCommonUser, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	abilities := []model.Ability{
		{Group: "default", Model: "phase5-gpt", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "claude-upstream", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "claude-upstream", ChannelId: 2, Enabled: true},
		{Group: "pro", Model: "claude-upstream", ChannelId: 3, Enabled: true},
		{Group: "pro", Model: "provider/qwen-mt-image-2.0", ChannelId: 3, Enabled: true},
		{Group: "pro", Model: "gemini-sync", ChannelId: 4, Enabled: true},
		{Group: "disabled", Model: "hidden-model", ChannelId: 5, Enabled: false},
	}
	require.NoError(t, db.Create(&abilities).Error)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("catalog-fixture-secret"))))
	SetApiRouter(engine)
	request := httptest.NewRequest(http.MethodGet, "/api/studio/models", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)

	request = httptest.NewRequest(http.MethodGet, "/api/studio/models", nil)
	request.Header.Set("Authorization", "Bearer "+user.AccessToken)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	var envelope struct {
		Success bool
		Data    json.RawMessage
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.True(t, envelope.Success)
	require.JSONEq(t, `{"group":"default","models":[
		{"name":"claude-upstream","category":"chat","groups":["default","pro"],"usable":true,"price":{"input":2000000,"output":6000000}},
		{"name":"gemini-sync","category":"chat","groups":["pro"],"usable":false},
		{"name":"phase5-gpt","category":"other","groups":["default"],"usable":true},
		{"name":"provider/qwen-mt-image-2.0","category":"image","groups":["pro"],"usable":false}
	]}`, string(envelope.Data))
}
