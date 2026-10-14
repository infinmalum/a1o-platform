package router

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/one-gateway/common"
	"github.com/infinmalum/one-gateway/common/client"
	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
	billingratio "github.com/infinmalum/one-gateway/relay/billing/ratio"
	"github.com/infinmalum/one-gateway/relay/channeltype"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSystemOneRouteBillsThroughLifecycle(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousClient := client.HTTPClient
	previousRedis, previousMemoryCache, previousRetries := common.RedisEnabled, config.MemoryCacheEnabled, config.RetryTimes
	previousSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open("file:systemone-route?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}); err != nil {
		t.Fatal(err)
	}
	model.DB, model.LOG_DB = db, db
	client.HTTPClient = &http.Client{}
	common.RedisEnabled, config.MemoryCacheEnabled = false, false
	common.UsingSQLite = true
	config.RetryTimes = 0
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		client.HTTPClient = previousClient
		common.RedisEnabled, config.MemoryCacheEnabled = previousRedis, previousMemoryCache
		common.UsingSQLite = previousSQLite
		config.RetryTimes = previousRetries
		_ = sqlDB.Close()
	})
	user := model.User{Username: "systemone-test", Status: model.UserStatusEnabled, Role: model.RoleAdminUser, Group: "default", Quota: 1000000}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := model.Token{UserId: user.Id, Key: "gateway-key", Name: "systemone", Status: model.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}

	systemOneResponse := `{"model":"jev-1.13.0","answers":{"is_urgent":{"type":"noul","noul":0.95},"department":{"type":"choice","choice":"billing","probabilities":{"billing":0.88,"technical":0.12,"sales":0.0},"confidence":0.81}},"usage":{"input_tokens":307,"output_tokens":20},"future":true}`
	upstreamError := `{"error":{"type":"invalid_request_error","message":"question criteria are malformed"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer typesafe-provider-key" {
			t.Errorf("SystemOne upstream got incorrect route or credentials: %s %v", r.URL, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"state":"fail"`) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, upstreamError)
			return
		}
		if !strings.Contains(string(body), `"model":"jev-1.13.0"`) || !strings.Contains(string(body), `"state":"payout failing"`) || !strings.Contains(string(body), `"is_urgent"`) {
			t.Errorf("SystemOne upstream lost request fields: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, systemOneResponse)
	}))
	defer upstream.Close()
	baseURL, mapping := upstream.URL, `{"jev-latest":"jev-1.13.0"}`
	channel := model.Channel{Type: channeltype.TypeSafe, Key: "typesafe-provider-key", Name: "typesafe", Status: model.ChannelStatusEnabled, Group: "default", Models: "jev-latest", BaseURL: &baseURL, ModelMapping: &mapping}
	if err := channel.Insert(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetRelayRouter(r)

	if billingratio.GetModelRatio("jev-latest", channeltype.TypeSafe) != 0.021 || billingratio.GetCompletionRatio("jev-latest", channeltype.TypeSafe) != 0 {
		t.Fatalf("jev pricing is wrong: %f %f", billingratio.GetModelRatio("jev-latest", channeltype.TypeSafe), billingratio.GetCompletionRatio("jev-latest", channeltype.TypeSafe))
	}

	quotaBefore, err := model.GetUserQuota(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(`{"model":"jev-latest","state":"payout failing","questions":{"is_urgent":{"type":"noul","instructions":"urgent?"},"department":{"type":"choice","instructions":"which team?","criteria":{"billing":"payments","technical":"bugs","sales":"pricing"}}}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-gateway-key")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != systemOneResponse {
		t.Fatalf("SystemOne response changed: status %d body %s", response.Code, response.Body.String())
	}
	// 307 input tokens at ratio 0.021 with a zero completion ratio: ceil(6.447) = 7.
	expectedQuota := int64(math.Ceil(billingratio.GetModelRatio("jev-latest", channeltype.TypeSafe) * billingratio.GetGroupRatio("default") * 307))
	quotaAfter, err := model.GetUserQuota(user.Id)
	if err != nil || quotaBefore-quotaAfter != expectedQuota {
		t.Fatalf("SystemOne request was not billed correctly: before %d after %d expected %d err %v", quotaBefore, quotaAfter, expectedQuota, err)
	}
	var log model.Log
	if err := db.Where("channel_id = ? AND type = ?", channel.Id, model.LogTypeConsume).First(&log).Error; err != nil || log.PromptTokens != 307 || log.CompletionTokens != 20 || int64(log.Quota) != expectedQuota {
		t.Fatalf("SystemOne consume log is wrong: %+v err %v", log, err)
	}

	quotaBeforeError, err := model.GetUserQuota(user.Id)
	if err != nil {
		t.Fatal(err)
	}
	errorRequest := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(`{"model":"jev-latest","state":"fail","questions":{"is_urgent":{"type":"noul","instructions":"urgent?"}}}`))
	errorRequest.Header.Set("Content-Type", "application/json")
	errorRequest.Header.Set("Authorization", "Bearer sk-gateway-key")
	errorResponse := httptest.NewRecorder()
	r.ServeHTTP(errorResponse, errorRequest)
	quotaAfterError, err := model.GetUserQuota(user.Id)
	if errorResponse.Code != http.StatusUnprocessableEntity || errorResponse.Body.String() != upstreamError || err != nil || quotaAfterError != quotaBeforeError {
		t.Fatalf("SystemOne upstream error was mapped or billed: status %d body %s before %d after %d err %v", errorResponse.Code, errorResponse.Body.String(), quotaBeforeError, quotaAfterError, err)
	}

	invalidRequest := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(`{"model":"jev-latest","state":"payout failing"}`))
	invalidRequest.Header.Set("Content-Type", "application/json")
	invalidRequest.Header.Set("Authorization", "Bearer sk-gateway-key")
	invalidResponse := httptest.NewRecorder()
	r.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest || !strings.Contains(invalidResponse.Body.String(), `"type":"invalid_request_error"`) {
		t.Fatalf("SystemOne validation error envelope changed: status %d body %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	unknownRequest := httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(`{"model":"orphan-model","state":"payout failing","questions":{"q":{"type":"noul","instructions":"?"}}}`))
	unknownRequest.Header.Set("Content-Type", "application/json")
	unknownRequest.Header.Set("Authorization", "Bearer sk-gateway-key")
	unknownResponse := httptest.NewRecorder()
	r.ServeHTTP(unknownResponse, unknownRequest)
	if unknownResponse.Code != http.StatusServiceUnavailable || !strings.Contains(unknownResponse.Body.String(), `"type":"server_error"`) {
		t.Fatalf("SystemOne request without a channel changed: status %d body %s", unknownResponse.Code, unknownResponse.Body.String())
	}
}
