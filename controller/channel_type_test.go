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

	"github.com/infinmalum/one-gateway/common"
	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
	"github.com/infinmalum/one-gateway/relay/channeltype"
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
	body := fmt.Sprintf(`{"type": %d, "name": "legacy", "key": "sk-test", "models": "gpt-4o-mini", "group": "default"}`, kind)
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

// TestChannelMutationRejectsRetiredTypes pins the entry guard: a channel
// created on a retired kind would be silently rewritten by the boot-time
// migration, so both handlers must refuse it up front and persist nothing.
func TestChannelMutationRejectsRetiredTypes(t *testing.T) {
	setupChannelTypeTest(t)
	checked := 0
	for kind := 0; kind < channeltype.Dummy; kind++ {
		from, to, retired := model.RetiredChannelType(kind)
		if !retired {
			continue
		}
		require.Equal(t, kind, from)
		require.NotEqual(t, from, to, "Tencent-style URL fixes must not count as retired")

		response := callChannelMutation(t, http.MethodPost, kind)
		require.Equal(t, false, response["success"], "kind %d must be refused", kind)
		require.Contains(t, response["message"], "已下线")

		response = callChannelMutation(t, http.MethodPut, kind)
		require.Equal(t, false, response["success"], "kind %d must be refused on update", kind)
		checked++
	}
	require.Greater(t, checked, 0, "the migration table must still list retired kinds")

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.Zero(t, count, "no channel may persist behind a refused type")
}

func TestChannelMutationRejectsOutOfRangeTypes(t *testing.T) {
	setupChannelTypeTest(t)
	for _, kind := range []int{-1, channeltype.Dummy, channeltype.Dummy + 7} {
		response := callChannelMutation(t, http.MethodPost, kind)
		require.Equal(t, false, response["success"], "kind %d must be refused", kind)
		require.Contains(t, response["message"], "非法的渠道类型")
	}
}

// TestAddChannelAcceptsLiveKinds keeps the guard from over-refusing: kinds
// that still ride the relay (the OpenAI wire kinds and Tencent, whose
// migration row only fixes legacy URLs) must keep creating fine.
func TestAddChannelAcceptsLiveKinds(t *testing.T) {
	setupChannelTypeTest(t)
	for _, kind := range []int{channeltype.OpenAI, channeltype.Zhipu, channeltype.Tencent, channeltype.Ollama, channeltype.Cohere} {
		response := callChannelMutation(t, http.MethodPost, kind)
		require.Equal(t, true, response["success"], "kind %d should be accepted: %v", kind, response["message"])
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 5, count)
}

// TestDashboardModelListSkipsRetiredTypes pins the dashboard listing: retired
// kinds ride ToAPIType onto the OpenAI adaptor, so without the explicit skip
// they would advertise the whole OpenAI preset despite being uncreatable.
func TestDashboardModelListSkipsRetiredTypes(t *testing.T) {
	require.NotNil(t, channelId2Models, "controller init must have built the listing")
	for _, kind := range []int{channeltype.PaLM, channeltype.Baidu, channeltype.Ali, channeltype.Xunfei, channeltype.AIProxyLibrary} {
		require.Empty(t, channelId2Models[kind], "retired kind %d must advertise no models", kind)
	}
	for _, kind := range []int{channeltype.OpenAI, channeltype.BaiduV2, channeltype.AliBailian} {
		require.NotEmpty(t, channelId2Models[kind], "live kind %d must keep its preset", kind)
	}
	// Zhipu/Tencent/Ollama legitimately ship empty presets (upstreams are
	// fetched live); the retired kinds above must match that emptiness.
	for _, kind := range []int{channeltype.Zhipu, channeltype.Tencent, channeltype.Ollama} {
		require.Empty(t, channelId2Models[kind], "live kind %d keeps shipping no preset", kind)
	}
}
