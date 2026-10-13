package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/infinmalum/one-gateway/common/helper"
	"gorm.io/gorm"
)

const (
	UpstreamAccountStatusEnabled          = 1 // don't use 0, 0 is the default value!
	UpstreamAccountStatusManuallyDisabled = 2
	UpstreamAccountStatusReauthRequired   = 3 // set when the provider rejects the refresh token
)

// ErrCredentialVersionConflict reports a lost compare-and-swap on the sealed
// credential document: another process rotated it first.
var ErrCredentialVersionConflict = errors.New("credential was updated concurrently")

// UpstreamAccount stores one provider subscription account connected by an
// administrator. The credential document is sealed by relay/credential and is
// never exposed through JSON.
type UpstreamAccount struct {
	Id                  int    `json:"id"`
	Name                string `json:"name" gorm:"index"`
	Provider            string `json:"provider" gorm:"type:varchar(64);index"`
	Profile             string `json:"profile" gorm:"type:varchar(64)"`
	AdminUserId         int    `json:"admin_user_id"`
	ExternalId          string `json:"external_id" gorm:"type:varchar(255)"`
	RegistrationId      string `json:"registration_id" gorm:"type:varchar(255)"`
	CredentialKind      string `json:"credential_kind" gorm:"type:varchar(32)"`
	EncryptedCredential string `json:"-" gorm:"type:text"`
	CredentialVersion   int64  `json:"credential_version" gorm:"bigint;default:0"`
	CredentialExpiresAt int64  `json:"credential_expires_at" gorm:"bigint;default:0"`
	Status              int    `json:"status" gorm:"default:1"`
	ConcurrencyLimit    int    `json:"concurrency_limit" gorm:"default:0"` // 0 means unlimited
	CreatedTime         int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime         int64  `json:"updated_time" gorm:"bigint"`
}

// UpstreamAccountDTO is the redacted API projection of an account: connection
// status and expiry are exposed, credential material never leaves the model.
type UpstreamAccountDTO struct {
	Id                  int    `json:"id"`
	Name                string `json:"name"`
	Provider            string `json:"provider"`
	Profile             string `json:"profile"`
	AdminUserId         int    `json:"admin_user_id"`
	ExternalId          string `json:"external_id"`
	RegistrationId      string `json:"registration_id"`
	CredentialKind      string `json:"credential_kind"`
	CredentialVersion   int64  `json:"credential_version"`
	CredentialExpiresAt int64  `json:"credential_expires_at"`
	Status              int    `json:"status"`
	ConcurrencyLimit    int    `json:"concurrency_limit"`
	CreatedTime         int64  `json:"created_time"`
	UpdatedTime         int64  `json:"updated_time"`
}

func (account *UpstreamAccount) DTO() UpstreamAccountDTO {
	return UpstreamAccountDTO{
		Id:                  account.Id,
		Name:                account.Name,
		Provider:            account.Provider,
		Profile:             account.Profile,
		AdminUserId:         account.AdminUserId,
		ExternalId:          account.ExternalId,
		RegistrationId:      account.RegistrationId,
		CredentialKind:      account.CredentialKind,
		CredentialVersion:   account.CredentialVersion,
		CredentialExpiresAt: account.CredentialExpiresAt,
		Status:              account.Status,
		ConcurrencyLimit:    account.ConcurrencyLimit,
		CreatedTime:         account.CreatedTime,
		UpdatedTime:         account.UpdatedTime,
	}
}

// UpstreamAccountCooldown persists one cooldown observation with its
// account/model scope. The hot path keeps its own in-memory view; these rows
// exist for observability and post-mortem analysis.
type UpstreamAccountCooldown struct {
	Id          int64  `json:"id"`
	AccountId   int    `json:"account_id" gorm:"index"`
	Model       string `json:"model" gorm:"type:varchar(255)"` // empty means account-wide
	Reason      string `json:"reason" gorm:"type:varchar(255)"`
	UntilTime   int64  `json:"until_time" gorm:"bigint;index"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

func GetAllUpstreamAccounts(startIdx int, num int) ([]*UpstreamAccountDTO, error) {
	var accounts []UpstreamAccount
	err := DB.Omit("encrypted_credential").Order("id desc").Limit(num).Offset(startIdx).Find(&accounts).Error
	if err != nil {
		return nil, err
	}
	dtos := make([]*UpstreamAccountDTO, 0, len(accounts))
	for i := range accounts {
		dtos = append(dtos, &UpstreamAccountDTO{})
		*dtos[i] = accounts[i].DTO()
	}
	return dtos, nil
}

func GetUpstreamAccountById(id int, selectCredential bool) (*UpstreamAccount, error) {
	if id <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var account UpstreamAccount
	query := DB
	if !selectCredential {
		query = query.Omit("encrypted_credential")
	}
	err := query.First(&account, "id = ?", id).Error
	return &account, err
}

func (account *UpstreamAccount) Insert() error {
	now := helper.GetTimestamp()
	account.CreatedTime = now
	account.UpdatedTime = now
	if account.Status == 0 {
		account.Status = UpstreamAccountStatusEnabled
	}
	return DB.Create(account).Error
}

// UpdateUpstreamAccountMetadata changes administrator-facing fields only.
func UpdateUpstreamAccountMetadata(id int, name string, concurrencyLimit int, status int) error {
	return DB.Model(&UpstreamAccount{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":             name,
		"concurrency_limit": concurrencyLimit,
		"status":           status,
		"updated_time":     helper.GetTimestamp(),
	}).Error
}

func UpdateUpstreamAccountStatus(id int, status int) error {
	return DB.Model(&UpstreamAccount{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       status,
		"updated_time": helper.GetTimestamp(),
	}).Error
}

// UpdateUpstreamAccountCredential atomically replaces the sealed credential
// document when expectedVersion still matches, and returns the new version.
func UpdateUpstreamAccountCredential(id int, expectedVersion int64, encrypted string, expiresAt int64) (int64, error) {
	result := DB.Model(&UpstreamAccount{}).
		Where("id = ? AND credential_version = ?", id, expectedVersion).
		Updates(map[string]interface{}{
			"encrypted_credential":  encrypted,
			"credential_version":    expectedVersion + 1,
			"credential_expires_at": expiresAt,
			"updated_time":          helper.GetTimestamp(),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := DB.Model(&UpstreamAccount{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return 0, err
		}
		if count == 0 {
			return 0, gorm.ErrRecordNotFound
		}
		return 0, ErrCredentialVersionConflict
	}
	return expectedVersion + 1, nil
}

func CountChannelsBoundToAccount(id int) (int64, error) {
	var count int64
	return count, DB.Model(&Channel{}).Where("upstream_account_id = ?", id).Count(&count).Error
}

func DeleteUpstreamAccountById(id int) error {
	bound, err := CountChannelsBoundToAccount(id)
	if err != nil {
		return err
	}
	if bound > 0 {
		return errors.New("channels still reference this account")
	}
	return DB.Delete(&UpstreamAccount{}, "id = ?", id).Error
}

func InsertUpstreamAccountCooldown(accountId int, modelName string, reason string, untilTime int64) error {
	cooldown := UpstreamAccountCooldown{
		AccountId:   accountId,
		Model:       modelName,
		Reason:      reason,
		UntilTime:   untilTime,
		CreatedTime: helper.GetTimestamp(),
	}
	return DB.Create(&cooldown).Error
}

// UpstreamOAuthHostIDKey stores the gateway host identifier in the option
// table without registering it in the user-facing OptionMap.
const UpstreamOAuthHostIDKey = "UpstreamOAuthHostID"

var upstreamOAuthHostIDMu sync.Mutex

// GetOrCreateUpstreamOAuthHostID returns the stable host identifier sent as
// ext_agent_host_id on Sign in with ChatGPT registrations. The provider
// requires one persistent identifier per host; it is created once and reused
// for every later sign-in of this gateway.
func GetOrCreateUpstreamOAuthHostID() (string, error) {
	upstreamOAuthHostIDMu.Lock()
	defer upstreamOAuthHostIDMu.Unlock()
	var option Option
	err := DB.Where(&Option{Key: UpstreamOAuthHostIDKey}).First(&option).Error
	if err == nil && option.Value != "" {
		return option.Value, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	hostID := "ogw-" + hex.EncodeToString(buf)
	if err := DB.Save(&Option{Key: UpstreamOAuthHostIDKey, Value: hostID}).Error; err != nil {
		return "", err
	}
	return hostID, nil
}
