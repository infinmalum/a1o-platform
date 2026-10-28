package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/helper"
	"github.com/infinmalum/a1o-platform/common/logger"
	"gorm.io/gorm"
)

const (
	ChannelStatusUnknown          = 0
	ChannelStatusEnabled          = 1 // don't use 0, 0 is the default value!
	ChannelStatusManuallyDisabled = 2 // also don't use 0
	ChannelStatusAutoDisabled     = 3
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"type:text"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              *string `json:"other"`   // DEPRECATED: please save config to field Config
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(32);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:varchar(1024);default:''"`
	Priority           *int64  `json:"priority" gorm:"bigint;default:0"`
	Config             string  `json:"config"`
	SystemPrompt       *string `json:"system_prompt" gorm:"type:text"`
	// UpstreamAccountID optionally binds the channel to a subscription
	// account instead of a per-channel API key; nil keeps plain API-key use.
	UpstreamAccountID *int `json:"upstream_account_id"`
}

type ChannelConfig struct {
	Region            string `json:"region,omitempty"`
	SK                string `json:"sk,omitempty"`
	AK                string `json:"ak,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	APIVersion        string `json:"api_version,omitempty"`
	LibraryID         string `json:"library_id,omitempty"`
	Plugin            string `json:"plugin,omitempty"`
	VertexAIProjectID string `json:"vertex_ai_project_id,omitempty"`
	VertexAIADC       string `json:"vertex_ai_adc,omitempty"`
}

func GetAllChannels(startIdx int, num int, scope string) ([]*Channel, error) {
	var channels []*Channel
	var err error
	switch scope {
	case "all":
		err = DB.Order("id desc").Find(&channels).Error
	case "disabled":
		err = DB.Order("id desc").Where("status = ? or status = ?", ChannelStatusAutoDisabled, ChannelStatusManuallyDisabled).Find(&channels).Error
	default:
		err = DB.Order("id desc").Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func SearchChannels(keyword string) (channels []*Channel, err error) {
	// A non-numeric keyword cannot match an id; 0 is no id, so only the name
	// prefix filter applies.
	id := 0
	if parsed, parseErr := strconv.Atoi(keyword); parseErr == nil {
		id = parsed
	}
	err = DB.Omit("key").Where("id = ? or name LIKE ?", id, keyword+"%").Find(&channels).Error
	return channels, err
}

func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(&channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(&channel, "id = ?", id).Error
	}
	return &channel, err
}

func BatchInsertChannels(channels []Channel) error {
	var err error
	err = DB.Create(&channels).Error
	if err != nil {
		return err
	}
	for _, channel_ := range channels {
		err = channel_.AddAbilities()
		if err != nil {
			return err
		}
	}
	groups := make([]string, 0, len(channels))
	for _, channel := range channels {
		groups = append(groups, channel.Group)
	}
	refreshChannelCache(groups...)
	return nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	return *channel.BaseURL
}

func (channel *Channel) GetModelMapping() map[string]string {
	if channel.ModelMapping == nil || *channel.ModelMapping == "" || *channel.ModelMapping == "{}" {
		return nil
	}
	modelMapping := make(map[string]string)
	err := json.Unmarshal([]byte(*channel.ModelMapping), &modelMapping)
	if err != nil {
		logger.SysError(fmt.Sprintf("failed to unmarshal model mapping for channel %d, error: %s", channel.Id, err.Error()))
		return nil
	}
	return modelMapping
}

func (channel *Channel) Insert() error {
	var err error
	err = DB.Create(channel).Error
	if err != nil {
		return err
	}
	err = channel.AddAbilities()
	if err == nil {
		refreshChannelCache(channel.Group)
	}
	return err
}

func (channel *Channel) Update() error {
	var previousGroup string
	DB.Model(&Channel{}).Where("id = ?", channel.Id).Pluck("group", &previousGroup)
	// Struct Updates silently skips zero values, so clearing base_url,
	// model_mapping, or resetting weight/priority to 0 never persisted.
	// Select the editable columns explicitly; server-maintained counters
	// (balance, used_quota, test_time, response_time) stay excluded so a
	// stale payload cannot rewind them.
	columns := []string{"type", "status", "name", "weight", "base_url", "other",
		"models", "group", "model_mapping", "priority", "config", "system_prompt",
		"upstream_account_id"}
	if channel.Key != "" {
		// GET /api/channel/:id omits the key, so edit payloads round-trip an
		// empty key: only rewrite it when a new one was actually entered.
		columns = append(columns, "key")
	}
	err := DB.Model(channel).Select(columns).Updates(channel).Error
	if err != nil {
		return err
	}
	DB.Model(channel).First(channel, "id = ?", channel.Id)
	err = channel.UpdateAbilities()
	if err == nil {
		refreshChannelCache(previousGroup, channel.Group)
	}
	return err
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     helper.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		logger.SysError("failed to update response time: " + err.Error())
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: helper.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		logger.SysError("failed to update balance: " + err.Error())
	}
}

func (channel *Channel) Delete() error {
	var previousGroup string
	DB.Model(&Channel{}).Where("id = ?", channel.Id).Pluck("group", &previousGroup)
	var err error
	err = DB.Delete(channel).Error
	if err != nil {
		return err
	}
	err = channel.DeleteAbilities()
	if err == nil {
		refreshChannelCache(previousGroup)
	}
	return err
}

func (channel *Channel) LoadConfig() (ChannelConfig, error) {
	var cfg ChannelConfig
	if channel.Config == "" {
		return cfg, nil
	}
	err := json.Unmarshal([]byte(channel.Config), &cfg)
	if err != nil {
		return cfg, err
	}
	return cfg, nil
}

func UpdateChannelStatusById(id int, status int) {
	var group string
	DB.Model(&Channel{}).Where("id = ?", id).Pluck("group", &group)
	err := UpdateAbilityStatus(id, status == ChannelStatusEnabled)
	if err != nil {
		logger.SysError("failed to update ability status: " + err.Error())
	}
	err = DB.Model(&Channel{}).Where("id = ?", id).Update("status", status).Error
	if err != nil {
		logger.SysError("failed to update channel status: " + err.Error())
	} else {
		refreshChannelCache(group)
	}
}

func UpdateChannelUsedQuota(id int, quota int64) {
	if config.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int64) {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		logger.SysError("failed to update channel used quota: " + err.Error())
	}
}

// ChannelHealthSummary counts channels by operational state for the admin
// dashboard. Only aggregates leave this type; channel keys never do.
type ChannelHealthSummary struct {
	Total              int64 `json:"total"`
	Enabled            int64 `json:"enabled"`
	ManuallyDisabled   int64 `json:"manually_disabled"`
	AutoDisabled       int64 `json:"auto_disabled"`
	LowBalance         int64 `json:"low_balance"`
	BalanceUnavailable int64 `json:"balance_unavailable"`
}

// GetChannelHealthSummary reports how many channels are serving, disabled, or
// running out of upstream balance. A channel whose balance was never fetched
// counts as BalanceUnavailable rather than empty, so providers that expose no
// balance are not reported as broke.
func GetChannelHealthSummary(lowBalanceThreshold float64) (summary ChannelHealthSummary, err error) {
	byStatus := []struct {
		status int
		target *int64
	}{
		{ChannelStatusEnabled, &summary.Enabled},
		{ChannelStatusManuallyDisabled, &summary.ManuallyDisabled},
		{ChannelStatusAutoDisabled, &summary.AutoDisabled},
	}
	for _, entry := range byStatus {
		if err = DB.Model(&Channel{}).Where("status = ?", entry.status).Count(entry.target).Error; err != nil {
			return summary, err
		}
	}
	if err = DB.Model(&Channel{}).Count(&summary.Total).Error; err != nil {
		return summary, err
	}
	if err = DB.Model(&Channel{}).
		Where("balance_updated_time > 0 AND balance >= 0 AND balance < ?", lowBalanceThreshold).
		Count(&summary.LowBalance).Error; err != nil {
		return summary, err
	}
	if err = DB.Model(&Channel{}).
		Where("balance_updated_time = 0 OR balance < 0").
		Count(&summary.BalanceUnavailable).Error; err != nil {
		return summary, err
	}
	return summary, nil
}

func DeleteDisabledChannel() (int64, error) {
	var groups []string
	DB.Model(&Channel{}).Where("status = ? or status = ?", ChannelStatusAutoDisabled, ChannelStatusManuallyDisabled).Pluck("group", &groups)
	result := DB.Where("status = ? or status = ?", ChannelStatusAutoDisabled, ChannelStatusManuallyDisabled).Delete(&Channel{})
	if result.Error == nil && result.RowsAffected > 0 {
		refreshChannelCache(groups...)
	}
	return result.RowsAffected, result.Error
}

func refreshChannelCache(groups ...string) {
	if common.RedisEnabled {
		seen := make(map[string]bool)
		for _, combinedGroups := range groups {
			for _, group := range strings.Split(combinedGroups, ",") {
				group = strings.TrimSpace(group)
				if group == "" || seen[group] {
					continue
				}
				seen[group] = true
				if err := common.RedisDel("group_models:" + group); err != nil {
					logger.SysError("failed to invalidate group models: " + err.Error())
				}
			}
		}
		if err := common.RDB.Incr(context.Background(), "channel_cache_version").Err(); err != nil {
			logger.SysError("failed to publish channel cache version: " + err.Error())
		}
	}
	if config.MemoryCacheEnabled {
		InitChannelCache()
	}
}
