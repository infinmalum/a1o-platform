package controller

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	relay "github.com/infinmalum/a1o-platform/relay"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/meta"
	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
)

// https://platform.openai.com/docs/api-reference/models/list

type OpenAIModelPermission struct {
	Id                 string  `json:"id"`
	Object             string  `json:"object"`
	Created            int     `json:"created"`
	AllowCreateEngine  bool    `json:"allow_create_engine"`
	AllowSampling      bool    `json:"allow_sampling"`
	AllowLogprobs      bool    `json:"allow_logprobs"`
	AllowSearchIndices bool    `json:"allow_search_indices"`
	AllowView          bool    `json:"allow_view"`
	AllowFineTuning    bool    `json:"allow_fine_tuning"`
	Organization       string  `json:"organization"`
	Group              *string `json:"group"`
	IsBlocking         bool    `json:"is_blocking"`
}

type OpenAIModels struct {
	Id         string                  `json:"id"`
	Object     string                  `json:"object"`
	Created    int                     `json:"created"`
	OwnedBy    string                  `json:"owned_by"`
	Permission []OpenAIModelPermission `json:"permission"`
	Root       string                  `json:"root"`
	Parent     *string                 `json:"parent"`
}

var channelId2Models map[int][]string

func init() {
	channelId2Models = make(map[int][]string)
	for i := 1; i < channeltype.Dummy; i++ {
		// A nil slice marshals as JSON null, which crashes frontend consumers
		// that index the listing and read .length; store real empty slices.
		if !channeltype.IsValid(i) {
			// Unassigned IDs in the range are not kinds.
			channelId2Models[i] = []string{}
			continue
		}
		adaptor := relay.GetAdaptor(channeltype.ToAPIType(i))
		if adaptor == nil {
			channelId2Models[i] = []string{}
			continue
		}
		meta := &meta.Meta{
			ChannelType: i,
		}
		adaptor.Init(meta)
		modelList := adaptor.GetModelList()
		if modelList == nil {
			// The compatible kind ships no presets (models are fetched from
			// the upstream); keep a real empty slice so the JSON is [] not null.
			modelList = []string{}
		}
		channelId2Models[i] = modelList
	}
}

func DashboardListModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channelId2Models,
	})
}

// ListAllModels backs the admin console channel form: the model universe is
// what the deployed channels actually serve, aggregated from the ability
// table, never a hardcoded preset list. The empty slice stays non-nil so the
// JSON is [] and frontend .map consumers cannot crash on null.
func ListAllModels(c *gin.Context) {
	modelNames, err := model.GetAllModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	list := make([]OpenAIModels, 0, len(modelNames))
	for _, name := range modelNames {
		list = append(list, OpenAIModels{
			Id:      name,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: "custom",
			Root:    name,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   list,
	})
}

func ListModels(c *gin.Context) {
	ctx := c.Request.Context()
	var availableModels []string
	if c.GetString(ctxkey.AvailableModels) != "" {
		availableModels = strings.Split(c.GetString(ctxkey.AvailableModels), ",")
	} else {
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ := model.CacheGetUserGroup(userId)
		availableModels, _ = model.CacheGetGroupModels(ctx, userGroup)
	}
	list := make([]OpenAIModels, 0, len(availableModels))
	for _, name := range availableModels {
		list = append(list, OpenAIModels{
			Id:      name,
			Object:  "model",
			Created: 1626777600,
			OwnedBy: "custom",
			Root:    name,
		})
	}
	c.JSON(200, gin.H{
		"object": "list",
		"data":   list,
	})
}

func RetrieveModel(c *gin.Context) {
	modelId := c.Param("model")
	// Presets no longer define which models exist: a model resolves when some
	// enabled channel serves it, which keeps the relay lookup consistent with
	// the dynamic model universe.
	exists, err := model.ModelExists(c.Request.Context(), modelId)
	if err != nil {
		Error := relaymodel.Error{
			Message: err.Error(),
			Type:    "service_unavailable",
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": Error,
		})
		return
	}
	if !exists {
		Error := relaymodel.Error{
			Message: fmt.Sprintf("The model '%s' does not exist", modelId),
			Type:    "invalid_request_error",
			Param:   "model",
			Code:    "model_not_found",
		}
		c.JSON(200, gin.H{
			"error": Error,
		})
		return
	}
	c.JSON(200, OpenAIModels{
		Id:      modelId,
		Object:  "model",
		Created: 1626777600,
		OwnedBy: "custom",
		Root:    modelId,
	})
}

func GetUserAvailableModels(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.GetInt(ctxkey.Id)
	userGroup, err := model.CacheGetUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	models, err := model.CacheGetGroupModels(ctx, userGroup)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
	return
}

// modelPrice is one row of the user-facing price list. Amounts are reported as
// quota per million tokens so the console renders them with the same helper as
// every other figure, honouring DisplayInCurrencyEnabled and QuotaPerUnit.
type modelPrice struct {
	Name                  string  `json:"name"`
	Priced                bool    `json:"priced"`
	ModelRatio            float64 `json:"model_ratio"`
	CompletionRatio       float64 `json:"completion_ratio"`
	QuotaPerMillion       int64   `json:"quota_per_million"`
	OutputQuotaPerMillion int64   `json:"output_quota_per_million"`
}

// GetUserPricing lists what the caller's group can use and what that group pays
// for it, so the console can show a price list without the admin option page.
// Models without a configured ratio are reported unpriced rather than given the
// billing fallback, which would advertise a price nobody set.
func GetUserPricing(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.GetInt(ctxkey.Id)
	userGroup, err := model.CacheGetUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	models, err := model.CacheGetGroupModels(ctx, userGroup)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	groupRatio := billingratio.GetGroupRatio(userGroup)
	prices := make([]modelPrice, 0, len(models))
	for _, name := range models {
		entry := modelPrice{Name: name}
		if ratio, ok := billingratio.LookupModelRatio(name); ok {
			completion := billingratio.GetCompletionRatio(name, 0)
			entry.Priced = true
			entry.ModelRatio = ratio
			entry.CompletionRatio = completion
			entry.QuotaPerMillion = int64(math.Round(ratio * groupRatio * 1e6))
			entry.OutputQuotaPerMillion = int64(math.Round(ratio * completion * groupRatio * 1e6))
		}
		prices = append(prices, entry)
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i].Name < prices[j].Name })
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"group":       userGroup,
			"group_ratio": groupRatio,
			"models":      prices,
		},
	})
	return
}
