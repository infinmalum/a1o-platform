package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/helper"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// profileChannelTypes maps a subscription profile onto the channel kinds its
// wire can serve, so a bound channel never points at an upstream the profile
// cannot reach.
func profileChannelTypes(profile string) []int {
	switch profile {
	case "chatgpt_plan_public":
		return []int{channeltype.OpenAI, channeltype.OpenAIResponses}
	case "anthropic_claude_code":
		return []int{channeltype.Anthropic}
	case "gemini_cli":
		return []int{channeltype.GeminiCLI}
	}
	return nil
}

// validateChannelAccountBinding rejects references to accounts that do not
// exist or whose profile cannot serve the channel kind, so a subscription
// channel never points at a stale or mismatched account.
func validateChannelAccountBinding(channel *model.Channel) error {
	if channel.UpstreamAccountID == nil {
		return nil
	}
	if *channel.UpstreamAccountID <= 0 {
		return fmt.Errorf("无效的上游账号 Id")
	}
	account, err := model.GetUpstreamAccountById(*channel.UpstreamAccountID, false)
	if err != nil {
		return fmt.Errorf("绑定的上游账号不存在：%d", *channel.UpstreamAccountID)
	}
	allowed := profileChannelTypes(account.Profile)
	if allowed == nil {
		return fmt.Errorf("上游账号 %d 的 Profile %s 不支持绑定渠道", account.Id, account.Profile)
	}
	matched := false
	for _, kind := range allowed {
		if channel.Type == kind {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("上游账号 %d（%s）需要绑定类型为 %v 的渠道", account.Id, account.Profile, allowed)
	}
	return nil
}

func GetAllChannels(c *gin.Context) {
	p, _ := strconv.Atoi(c.Query("p"))
	if p < 0 {
		p = 0
	}
	channels, err := model.GetAllChannels(p*config.ItemsPerPage, config.ItemsPerPage, "limited")
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
		"data":    channels,
	})
	return
}

func SearchChannels(c *gin.Context) {
	keyword := c.Query("keyword")
	channels, err := model.SearchChannels(keyword)
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
		"data":    channels,
	})
	return
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel, err := model.GetChannelById(id, false)
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
		"data":    channel,
	})
	return
}

// validateChannelType rejects channel kinds that do not exist.
func validateChannelType(channel *model.Channel) error {
	if !channeltype.IsValid(channel.Type) {
		return fmt.Errorf("非法的渠道类型: %d", channel.Type)
	}
	return nil
}

func AddChannel(c *gin.Context) {
	channel := model.Channel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := validateChannelType(&channel); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if _, err := channel.LoadConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := validateChannelAccountBinding(&channel); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	channel.CreatedTime = helper.GetTimestamp()
	keys := strings.Split(channel.Key, "\n")
	channels := make([]model.Channel, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		localChannel := channel
		localChannel.Key = key
		channels = append(channels, localChannel)
	}
	err = model.BatchInsertChannels(channels)
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
	})
	return
}

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	channel := model.Channel{Id: id}
	err := channel.Delete()
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
	})
	return
}

func DeleteDisabledChannel(c *gin.Context) {
	rows, err := model.DeleteDisabledChannel()
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
		"data":    rows,
	})
	return
}

func UpdateChannel(c *gin.Context) {
	channel := model.Channel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := validateChannelType(&channel); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if _, err := channel.LoadConfig(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := validateChannelAccountBinding(&channel); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	err = channel.Update()
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
		"data":    channel,
	})
	return
}
