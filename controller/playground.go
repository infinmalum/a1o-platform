package controller

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

func GetPlaygroundUsage(c *gin.Context) {
	var usage struct {
		Quota            int    `json:"quota"`
		PromptTokens     int    `json:"prompt_tokens"`
		CompletionTokens int    `json:"completion_tokens"`
		UsageState       string `json:"usage_state,omitempty"`
		ChargeBasis      string `json:"charge_basis,omitempty"`
		Interrupted      bool   `json:"interrupted,omitempty"`
	}
	result := model.LOG_DB.WithContext(c.Request.Context()).Model(&model.Log{}).
		Select("quota", "prompt_tokens", "completion_tokens", "usage_state", "charge_basis", "interrupted").
		Where("user_id = ? AND type = ? AND token_name = ? AND request_id = ? AND created_at >= ?",
			c.GetInt(ctxkey.Id), model.LogTypeConsume, model.ChatTokenName, c.Param("requestId"), time.Now().Add(-time.Hour).Unix()).
		Limit(1).Find(&usage)
	if result.Error != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if result.RowsAffected == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, usage)
}
