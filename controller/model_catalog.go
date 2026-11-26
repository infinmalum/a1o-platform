package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
)

type catalogModel struct {
	Name     string          `json:"name"`
	Category string          `json:"category"`
	Groups   []string        `json:"groups"`
	Usable   bool            `json:"usable"`
	Price    *catalogPricing `json:"price,omitempty"`
}

type catalogPricing struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

func GetStudioModelCatalog(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	group, err := model.CacheGetUserGroup(c.GetInt(ctxkey.Id))
	if err != nil {
		logger.SysError("read model catalog user group: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false})
		return
	}
	rows, err := model.ListEnabledModelGroups(c.Request.Context())
	if err != nil {
		logger.SysError("list model catalog: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false})
		return
	}

	models := make([]catalogModel, 0)
	groupRatio := billingratio.GetGroupRatio(group)
	for _, row := range rows {
		if len(models) == 0 || models[len(models)-1].Name != row.Model {
			models = append(models, catalogModel{Name: row.Model, Category: catalogCategory(row.Model)})
		}
		entry := &models[len(models)-1]
		entry.Groups = append(entry.Groups, row.Group)
		if row.Group == group {
			entry.Usable = true
			if entry.Category == "chat" {
				price := tokenModelPrice(row.Model, groupRatio)
				if price.Priced {
					entry.Price = &catalogPricing{Input: price.QuotaPerMillion, Output: price.OutputQuotaPerMillion}
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"group": group, "models": models}})
}

// Channels do not declare model capabilities. Classify known identifiers and
// leave unknown aliases uncategorized instead of promising an operation.
func catalogCategory(name string) string {
	name = strings.ToLower(name)
	if index := strings.LastIndexByte(name, '/'); index >= 0 {
		name = name[index+1:]
	}
	switch {
	case name == studioVideoModel, arkVideoName(name), catalogHasPrefix(name, "veo-", "sora-", "wan2.1-t2v", "wan2.2-i2v", "kling-", "hunyuanvideo"):
		return "video"
	case arkImageName(name), isGeminiImageModel(name), catalogHasPrefix(name, "gpt-image-", "dall-e-", "imagen-", "qwen-image", "qwen-mt-image", "wanx-", "stable-diffusion", "flux-", "flux."):
		return "image"
	case strings.Contains(name, "embedding"), catalogHasPrefix(name, "bge-", "e5-", "rerank-"), strings.Contains(name, "reranker"):
		return "retrieval"
	case strings.Contains(name, "audio"), strings.Contains(name, "transcribe"), strings.Contains(name, "realtime"), catalogHasPrefix(name, "whisper", "tts-", "cosyvoice", "paraformer", "qwen3-asr"):
		return "audio"
	case catalogHasPrefix(name, "gpt-", "chatgpt-", "o1", "o3", "o4", "claude-", "gemini-", "deepseek-", "qwen", "qwq-", "llama-", "mistral-", "mixtral-", "grok-", "glm-", "kimi-", "moonshot-", "doubao-", "gemma-"):
		return "chat"
	default:
		return "other"
	}
}

func catalogHasPrefix(name string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
