package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

func setupChannelTypeTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemoryCache, previousSQLite := common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = previousRedis, previousMemoryCache, previousSQLite
	})
	return db
}

func callChannelMutation(t *testing.T, method string, kind int) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"type": %d, "name": "test", "key": "sk-test", "models": "gpt-4o-mini", "group": "default"}`, kind)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/api/channel/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		AddChannel(c)
	} else {
		UpdateChannel(c)
	}
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

// TestChannelMutationRejectsNonexistentTypes pins the entry guard: kinds that
// do not exist — out-of-range values and the vacant retired slots in the ID
// space — must be refused and persist nothing.
func TestChannelMutationRejectsNonexistentTypes(t *testing.T) {
	setupChannelTypeTest(t)
	for _, kind := range []int{-1, 0, 2, 36, 51, channeltype.Dummy, channeltype.Dummy + 7} {
		response := callChannelMutation(t, http.MethodPost, kind)
		require.Equal(t, false, response["success"], "kind %d must be refused", kind)
		require.Contains(t, response["message"], "非法的渠道类型")

		response = callChannelMutation(t, http.MethodPut, kind)
		require.Equal(t, false, response["success"], "kind %d must be refused on update", kind)
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.Zero(t, count, "no channel may persist behind a refused type")
}

// TestAddChannelAcceptsLiveKinds keeps the guard from over-refusing: the
// surviving kinds (the OpenAI wire family plus the provider adapters that
// cannot be expressed as a custom endpoint) must keep creating fine.
func TestAddChannelAcceptsLiveKinds(t *testing.T) {
	setupChannelTypeTest(t)
	for _, kind := range []int{channeltype.OpenAI, channeltype.Azure, channeltype.Minimax, channeltype.OpenAICompatible, channeltype.Anthropic} {
		response := callChannelMutation(t, http.MethodPost, kind)
		require.Equal(t, true, response["success"], "kind %d should be accepted: %v", kind, response["message"])
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 5, count)
}

// TestDashboardModelList pins the dashboard listing: model presets are gone
// (models come from the upstream fetch or manual entry), so every kind
// advertises an empty-but-real list. nil would marshal as JSON null and
// crash the console's per-type model picker; a non-empty roster would
// auto-fill channels with years-old model names.
func TestDashboardModelList(t *testing.T) {
	require.NotNil(t, channelId2Models, "controller init must have built the listing")
	for kind := 1; kind < channeltype.Dummy; kind++ {
		require.NotNil(t, channelId2Models[kind], "kind %d must not marshal as null", kind)
		require.Empty(t, channelId2Models[kind], "kind %d must not ship stale presets", kind)
	}
}
