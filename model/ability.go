package model

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/utils"
)

// ChannelSource filters channel candidates by their credential source. The
// zero value keeps the historical API-key-only behavior.
type ChannelSource int

const (
	ChannelSourceAPIOnly ChannelSource = iota
	ChannelSourceSubscriptionOnly
	ChannelSourceAny
)

// applyChannelSource narrows a channels query to the requested source.
func applyChannelSource(query *gorm.DB, source ChannelSource) *gorm.DB {
	switch source {
	case ChannelSourceAPIOnly:
		return query.Where("upstream_account_id IS NULL")
	case ChannelSourceSubscriptionOnly:
		return query.Where("upstream_account_id IS NOT NULL")
	}
	return query
}

// GetRandomSatisfiedChannelByType selects a channel that can receive a native
// protocol request. A mixed-provider group must not randomly choose a channel
// that requires an unimplemented cross-protocol conversion.
func GetRandomSatisfiedChannelByType(group, model string, channelType int, ignoreFirstPriority bool) (*Channel, error) {
	return GetRandomSatisfiedChannelByTypesExcluding(group, model, []int{channelType}, ignoreFirstPriority, nil, ChannelSourceAPIOnly)
}

// GetRandomSatisfiedChannelExcluding serves protocol-neutral retries for
// legacy routes, whose provider type is selected from all compatible channels.
func GetRandomSatisfiedChannelExcluding(group, model string, ignoreFirstPriority bool, excluded map[int]bool, source ChannelSource) (*Channel, error) {
	return GetRandomSatisfiedChannelByTypesExcluding(group, model, nil, ignoreFirstPriority, excluded, source)
}

// GetRandomSatisfiedChannelByTypeExcluding chooses a compatible channel that
// has not already failed during the current native request.
func GetRandomSatisfiedChannelByTypeExcluding(group, model string, channelType int, ignoreFirstPriority bool, excluded map[int]bool, source ChannelSource) (*Channel, error) {
	return GetRandomSatisfiedChannelByTypesExcluding(group, model, []int{channelType}, ignoreFirstPriority, excluded, source)
}

// GetRandomSatisfiedChannelByTypesExcluding chooses a compatible channel whose
// type is one of channelTypes (empty means any type) and that has not already
// failed during the current native request. The list lets one protocol pick
// from every channel kind that speaks its wire format.
func GetRandomSatisfiedChannelByTypesExcluding(group, model string, channelTypes []int, ignoreFirstPriority bool, excluded map[int]bool, source ChannelSource) (*Channel, error) {
	return GetSatisfiedChannel(group, model, channelTypes, ignoreFirstPriority, excluded, source, "")
}

func GetSatisfiedChannel(group, model string, channelTypes []int, ignoreFirstPriority bool, excluded map[int]bool, source ChannelSource, affinity string) (*Channel, error) {
	var abilities []Ability
	groupColumn := "`group`"
	if common.UsingPostgreSQL {
		groupColumn = `"group"`
	}
	if err := DB.Where("enabled = ? AND model = ?", true, model).Where(groupColumn+" = ?", group).Find(&abilities).Error; err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		return nil, errors.New("channel not found")
	}
	ids := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		ids = append(ids, ability.ChannelId)
	}
	var channels []Channel
	query := DB.Where("id IN ? AND status = ?", ids, ChannelStatusEnabled)
	if len(channelTypes) > 0 {
		query = query.Where("type IN ?", channelTypes)
	}
	query = applyChannelSource(query, source)
	if err := query.Find(&channels).Error; err != nil {
		return nil, err
	}
	available := channels[:0]
	for _, channel := range channels {
		if !excluded[channel.Id] {
			available = append(available, channel)
		}
	}
	if len(available) == 0 {
		return nil, errors.New("channel not found")
	}
	sort.Slice(available, func(i, j int) bool { return available[i].GetPriority() > available[j].GetPriority() })
	firstPriority := available[0].GetPriority()
	end := len(available)
	if firstPriority > 0 {
		for i := range available {
			if available[i].GetPriority() != firstPriority {
				end = i
				break
			}
		}
	}
	if ignoreFirstPriority && end < len(available) {
		available = available[end:]
	} else {
		available = available[:end]
	}
	index := selectChannelIndex(len(available), affinity, func(i int) int { return available[i].Id })
	return &available[index], nil
}

func selectChannelIndex(count int, affinity string, idAt func(int) int) int {
	if affinity == "" {
		return rand.Intn(count)
	}
	// Rendezvous hashing keeps the winner stable when an unrelated candidate
	// is disabled and selects the next eligible candidate on retry.
	var highest uint64
	var selected int
	for i := range count {
		id := idAt(i)
		digest := sha256.Sum256([]byte(affinity + ":" + strconv.Itoa(id)))
		score := binary.BigEndian.Uint64(digest[:8])
		if i == 0 || score > highest || score == highest && id < idAt(selected) {
			selected, highest = i, score
		}
	}
	return selected
}

type Ability struct {
	Group     string `json:"group" gorm:"type:varchar(32);primaryKey;autoIncrement:false"`
	Model     string `json:"model" gorm:"primaryKey;autoIncrement:false"`
	ChannelId int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool   `json:"enabled"`
	Priority  *int64 `json:"priority" gorm:"bigint;default:0;index"`
}

func (channel *Channel) AddAbilities() error {
	models_ := strings.Split(channel.Models, ",")
	models_ = utils.DeDuplication(models_)
	groups_ := strings.Split(channel.Group, ",")
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == ChannelStatusEnabled,
				Priority:  channel.Priority,
			}
			abilities = append(abilities, ability)
		}
	}
	return DB.Create(&abilities).Error
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities() error {
	// A quick and dirty way to update abilities
	// First delete all abilities of this channel
	err := channel.DeleteAbilities()
	if err != nil {
		return err
	}
	// Then add new abilities
	err = channel.AddAbilities()
	if err != nil {
		return err
	}
	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func GetGroupModels(ctx context.Context, group string) ([]string, error) {
	groupCol := "`group`"
	trueVal := "1"
	if common.UsingPostgreSQL {
		groupCol = `"group"`
		trueVal = "true"
	}
	var models []string
	err := DB.Model(&Ability{}).Distinct("model").Where(groupCol+" = ? and enabled = "+trueVal, group).Pluck("model", &models).Error
	if err != nil {
		return nil, err
	}
	if models == nil {
		// An empty group must marshal as [] in API responses, not null.
		models = []string{}
	}
	sort.Strings(models)
	return models, err
}

// ListEnabledModelGroups returns one row per model and group, regardless of
// how many enabled channels offer the same model.
func ListEnabledModelGroups(ctx context.Context) ([]Ability, error) {
	var rows []Ability
	err := DB.WithContext(ctx).Where(&Ability{Enabled: true}).Distinct("model", "group").
		Order("model").Order(clause.OrderByColumn{Column: clause.Column{Name: "group"}}).Find(&rows).Error
	return rows, err
}

// GetAllModels lists the distinct models served by any enabled channel. With
// the preset model lists removed, this is the only "all known models" universe
// the console and the relay model listings can honestly report.
func GetAllModels(ctx context.Context) ([]string, error) {
	trueVal := "1"
	if common.UsingPostgreSQL {
		trueVal = "true"
	}
	var models []string
	err := DB.Model(&Ability{}).Distinct("model").Where("enabled = "+trueVal).Pluck("model", &models).Error
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []string{}
	}
	sort.Strings(models)
	return models, nil
}

// ModelExists reports whether any enabled channel serves the model; the relay
// per-model lookup needs this because presets no longer define the universe.
func ModelExists(ctx context.Context, name string) (bool, error) {
	trueVal := "1"
	if common.UsingPostgreSQL {
		trueVal = "true"
	}
	var count int64
	err := DB.WithContext(ctx).Model(&Ability{}).Where("model = ? and enabled = "+trueVal, name).Count(&count).Error
	return count > 0, err
}
