package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/common/ctxkey"
	"github.com/infinmalum/one-gateway/model"
	"github.com/infinmalum/one-gateway/relay/credential"
	"github.com/infinmalum/one-gateway/relay/upstream"
)

type upstreamAccountPayload struct {
	Id               int             `json:"id"`
	Name             string          `json:"name"`
	Provider         string          `json:"provider"`
	Profile          string          `json:"profile"`
	ExternalId       string          `json:"external_id"`
	RegistrationId   string          `json:"registration_id"`
	ConcurrencyLimit int             `json:"concurrency_limit"`
	Status           *int            `json:"status"`
	Credential       json.RawMessage `json:"credential"`
}

func GetAllUpstreamAccounts(c *gin.Context) {
	p, _ := strconv.Atoi(c.Query("p"))
	if p < 0 {
		p = 0
	}
	accounts, err := model.GetAllUpstreamAccounts(p*config.ItemsPerPage, config.ItemsPerPage)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": accounts})
}

func GetUpstreamAccount(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	account, err := model.GetUpstreamAccountById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	dto := account.DTO()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": dto})
}

// storeForWrite returns the configured credential store or rejects the
// request with an actionable message.
func storeForWrite(c *gin.Context) (*credential.Store, bool) {
	store, err := credential.DefaultStore()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "凭据加密密钥配置无效：" + err.Error()})
		return nil, false
	}
	if store == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "未配置凭据加密密钥（环境变量 UPSTREAM_CREDENTIAL_KEYS），无法存储订阅账号凭据"})
		return nil, false
	}
	return store, true
}

func AddUpstreamAccount(c *gin.Context) {
	var payload upstreamAccountPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if payload.Name == "" || payload.Profile == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "账号名称与 Profile 不能为空"})
		return
	}
	profile, ok := upstream.Lookup(payload.Profile)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "未知的 Profile：" + payload.Profile})
		return
	}
	if !profile.Subscription {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Profile " + payload.Profile + " 不支持账号接入"})
		return
	}
	if payload.Provider != profile.Provider {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("Provider 必须为 %s", profile.Provider)})
		return
	}
	if len(payload.Credential) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少凭据文档"})
		return
	}
	doc, err := credential.ParseDocument(payload.Credential)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if doc.Kind != profile.CredentialKind {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("凭据类型必须为 %s", profile.CredentialKind)})
		return
	}
	store, ok := storeForWrite(c)
	if !ok {
		return
	}
	// Seal the original bytes so provider-specific fields survive round trips.
	sealed, err := store.Seal(payload.Credential)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	adminId := c.GetInt(ctxkey.Id)
	account := model.UpstreamAccount{
		Name:                payload.Name,
		Provider:            payload.Provider,
		Profile:             payload.Profile,
		AdminUserId:         adminId,
		ExternalId:          payload.ExternalId,
		RegistrationId:      payload.RegistrationId,
		CredentialKind:      doc.Kind,
		EncryptedCredential: sealed,
		CredentialVersion:   1,
		CredentialExpiresAt: doc.ExpiresAt(),
		ConcurrencyLimit:    payload.ConcurrencyLimit,
	}
	if err := account.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordLog(c.Request.Context(), adminId, model.LogTypeManage,
		fmt.Sprintf("管理员创建了上游账号 %s（id: %d, provider: %s, profile: %s）", account.Name, account.Id, account.Provider, account.Profile))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": account.DTO()})
}

func UpdateUpstreamAccount(c *gin.Context) {
	var payload upstreamAccountPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if payload.Id <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的账号 Id"})
		return
	}
	status := 0
	if payload.Status != nil {
		switch *payload.Status {
		case model.UpstreamAccountStatusEnabled, model.UpstreamAccountStatusManuallyDisabled:
			status = *payload.Status
		default:
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的账号状态"})
			return
		}
	}
	if status == 0 {
		account, err := model.GetUpstreamAccountById(payload.Id, false)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		status = account.Status
	}
	if err := model.UpdateUpstreamAccountMetadata(payload.Id, payload.Name, payload.ConcurrencyLimit, status); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	adminId := c.GetInt(ctxkey.Id)
	model.RecordLog(c.Request.Context(), adminId, model.LogTypeManage,
		fmt.Sprintf("管理员更新了上游账号（id: %d, name: %s, 并发上限: %d, 状态: %d）", payload.Id, payload.Name, payload.ConcurrencyLimit, status))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func UpdateUpstreamAccountCredential(c *gin.Context) {
	var payload upstreamAccountPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if payload.Id <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的账号 Id"})
		return
	}
	if len(payload.Credential) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少凭据文档"})
		return
	}
	account, err := model.GetUpstreamAccountById(payload.Id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	doc, err := credential.ParseDocument(payload.Credential)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if doc.Kind != account.CredentialKind {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("凭据类型必须为 %s", account.CredentialKind)})
		return
	}
	store, ok := storeForWrite(c)
	if !ok {
		return
	}
	sealed, err := store.Seal(payload.Credential)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if _, err := model.UpdateUpstreamAccountCredential(payload.Id, account.CredentialVersion, sealed, doc.ExpiresAt()); err != nil {
		if err == model.ErrCredentialVersionConflict {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "凭据刚刚被其他进程更新，请刷新后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if account.Status == model.UpstreamAccountStatusReauthRequired {
		// A fresh credential replaces the revoked one; the account is usable
		// again until the provider says otherwise.
		if err := model.UpdateUpstreamAccountStatus(payload.Id, model.UpstreamAccountStatusEnabled); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	adminId := c.GetInt(ctxkey.Id)
	model.RecordLog(c.Request.Context(), adminId, model.LogTypeManage,
		fmt.Sprintf("管理员重新导入了上游账号 %s 的凭据（id: %d）", account.Name, account.Id))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteUpstreamAccount(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	account, err := model.GetUpstreamAccountById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.DeleteUpstreamAccountById(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "删除失败：" + err.Error()})
		return
	}
	adminId := c.GetInt(ctxkey.Id)
	model.RecordLog(c.Request.Context(), adminId, model.LogTypeManage,
		fmt.Sprintf("管理员删除了上游账号 %s（id: %d）", account.Name, account.Id))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
