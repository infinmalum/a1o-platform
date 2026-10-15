package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/common/ctxkey"
	"github.com/infinmalum/one-gateway/model"
)

func setupBillingTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
	})
	return db
}

// TestGetSubscriptionIncludesUsedQuota pins the billing math: the subscription
// total is remaining plus used quota. The used quota used to be fetched only
// when the remaining query failed, so the advertised balance was always low.
func TestGetSubscriptionIncludesUsedQuota(t *testing.T) {
	db := setupBillingTest(t)
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "bill-user", Password: "password123", Quota: 100, UsedQuota: 30}).Error)

	previousTokenStat, previousCurrency := config.DisplayTokenStatEnabled, config.DisplayInCurrencyEnabled
	config.DisplayTokenStatEnabled, config.DisplayInCurrencyEnabled = false, false
	t.Cleanup(func() {
		config.DisplayTokenStatEnabled, config.DisplayInCurrencyEnabled = previousTokenStat, previousCurrency
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/dashboard/billing/subscription", nil)
	c.Set(ctxkey.Id, 7)
	GetSubscription(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var subscription OpenAISubscriptionResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &subscription))
	require.Equal(t, float64(130), subscription.HardLimitUSD)
	require.Equal(t, float64(130), subscription.SoftLimitUSD)
}

func TestGetUsageReportsUsedQuota(t *testing.T) {
	db := setupBillingTest(t)
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "bill-user", Password: "password123", Quota: 100, UsedQuota: 30}).Error)

	previousTokenStat, previousCurrency := config.DisplayTokenStatEnabled, config.DisplayInCurrencyEnabled
	config.DisplayTokenStatEnabled, config.DisplayInCurrencyEnabled = false, false
	t.Cleanup(func() {
		config.DisplayTokenStatEnabled, config.DisplayInCurrencyEnabled = previousTokenStat, previousCurrency
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/dashboard/billing/usage", nil)
	c.Set(ctxkey.Id, 7)
	GetUsage(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var usage OpenAIUsageResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &usage))
	require.Equal(t, float64(3000), usage.TotalUsage) // 30 quota, reported ×100
}
