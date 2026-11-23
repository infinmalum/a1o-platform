package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/blacklist"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/common/network"
	"github.com/infinmalum/a1o-platform/model"
	"gorm.io/gorm"
)

func authHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	id, _ := session.Get("id").(int)
	var user *model.User
	if id > 0 {
		// Cookies can outlive account deletion or an admin role change.
		var account model.User
		err := model.DB.WithContext(c.Request.Context()).Select("id", "username", "role", "status").Take(&account, id).Error
		if err == nil {
			user = &account
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Authentication unavailable"})
			return
		}
	}
	if id == 0 {
		// Check access token
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.Header("X-Studio-Session-Required", "1")
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，未登录且未提供 access token",
			})
			c.Abort()
			return
		}
		user = model.ValidateAccessToken(accessToken)
	}

	if user == nil || user.Status != model.UserStatusEnabled || blacklist.IsUserBanned(user.Id) {
		if id > 0 {
			session.Clear()
			_ = session.Save()
		}
		c.Header("X-Studio-Session-Required", "1")
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "账户不可用",
			"code":    "studio_account_disabled",
		})
		c.Abort()
		return
	}
	if user.Role < minRole {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权进行此操作，权限不足",
		})
		c.Abort()
		return
	}
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	c.Set("id", user.Id)
	c.Next()
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, model.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, model.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, model.RoleRootUser)
	}
}

func TokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		key := c.Request.Header.Get("Authorization")
		if key == "" {
			switch {
			case c.Request.URL.Path == "/v1/messages":
				key = c.Request.Header.Get("x-api-key")
			case isNativeGeminiRequest(c):
				key = c.Request.Header.Get("x-goog-api-key")
			}
		}
		key = strings.TrimPrefix(key, "Bearer ")
		key = strings.TrimPrefix(key, "sk-")
		token, err := model.ValidateUserToken(key)
		var channelFromToken string
		if err != nil {
			// An administrator may pin a channel by appending its ID to the
			// token. Try the complete key first so hyphens in real tokens
			// remain valid.
			if split := strings.LastIndexByte(key, '-'); split >= 0 {
				if candidate, candidateErr := model.ValidateUserToken(key[:split]); candidateErr == nil {
					token, err = candidate, nil
					channelFromToken = key[split+1:]
				}
			}
		}
		if err != nil {
			abortWithMessage(c, http.StatusUnauthorized, err.Error())
			return
		}
		if token.Subnet != nil && *token.Subnet != "" {
			if !network.IsIpInSubnets(ctx, c.ClientIP(), *token.Subnet) {
				abortWithMessage(c, http.StatusForbidden, fmt.Sprintf("该令牌只能在指定网段使用：%s，当前 ip：%s", *token.Subnet, c.ClientIP()))
				return
			}
		}
		userEnabled, err := model.IsUserEnabled(token.UserId)
		if err != nil {
			abortWithMessage(c, http.StatusInternalServerError, err.Error())
			return
		}
		if !userEnabled || blacklist.IsUserBanned(token.UserId) {
			abortWithMessage(c, http.StatusForbidden, "用户已被封禁")
			return
		}
		requestModel, err := getRequestModel(c)
		if err != nil && shouldCheckModel(c) {
			abortWithMessage(c, http.StatusBadRequest, err.Error())
			return
		}
		c.Set(ctxkey.RequestModel, requestModel)
		if token.Models != nil && *token.Models != "" {
			c.Set(ctxkey.AvailableModels, *token.Models)
			if requestModel != "" && !isModelInList(requestModel, *token.Models) {
				abortWithMessage(c, http.StatusForbidden, fmt.Sprintf("该令牌无权使用模型：%s", requestModel))
				return
			}
		}
		c.Set(ctxkey.Id, token.UserId)
		c.Set(ctxkey.TokenId, token.Id)
		c.Set(ctxkey.TokenName, token.Name)
		if channelFromToken != "" {
			if model.IsAdmin(token.UserId) {
				c.Set(ctxkey.SpecificChannelId, channelFromToken)
			} else {
				abortWithMessage(c, http.StatusForbidden, "普通用户不支持指定渠道")
				return
			}
		}

		// set channel id for proxy relay
		if channelId := c.Param("channelid"); channelId != "" {
			if !model.IsAdmin(token.UserId) {
				abortWithMessage(c, http.StatusForbidden, "普通用户不支持指定渠道")
				return
			}
			c.Set(ctxkey.SpecificChannelId, channelId)
		}

		c.Next()
	}
}

func shouldCheckModel(c *gin.Context) bool {
	if strings.HasPrefix(c.Request.URL.Path, "/v1/completions") {
		return true
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/chat/completions") {
		return true
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		return true
	}
	if c.Request.URL.Path == "/v1/moderations" {
		return true
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images") {
		return true
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		return true
	}
	if c.Request.URL.Path == "/v1/messages" || c.Request.URL.Path == "/v1/responses" || isNativeGeminiRequest(c) {
		return true
	}
	if c.Request.URL.Path == "/v1/systemone" {
		return true
	}
	return false
}
