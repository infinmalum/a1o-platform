package model

import (
	"errors"
	"slices"
	"strings"

	"github.com/infinmalum/a1o-platform/common/config"
	"gorm.io/gorm/clause"
)

const studioDisabledLanguagesOption = "StudioDisabledLanguages"

var studioLanguageCodes = []string{"zh", "en", "fr", "de", "es", "ru", "uk", "be", "fa", "ar"}

type StudioLanguage struct {
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
}

func StudioLanguages() []StudioLanguage {
	config.OptionMapRWMutex.RLock()
	disabled := strings.Split(config.OptionMap[studioDisabledLanguagesOption], ",")
	config.OptionMapRWMutex.RUnlock()

	languages := make([]StudioLanguage, 0, len(studioLanguageCodes))
	for _, code := range studioLanguageCodes {
		languages = append(languages, StudioLanguage{Code: code, Enabled: !slices.Contains(disabled, code)})
	}
	return languages
}

func validateStudioDisabledLanguages(value string) error {
	if value == "" {
		return nil
	}
	disabled := strings.Split(value, ",")
	seen := make(map[string]bool, len(disabled))
	for _, code := range disabled {
		if !slices.Contains(studioLanguageCodes, code) || seen[code] {
			return errors.New("invalid Studio language list")
		}
		seen[code] = true
	}
	if len(seen) == len(studioLanguageCodes) {
		return errors.New("at least one Studio language must remain enabled")
	}
	return nil
}

func updateStudioDisabledLanguages(value string) error {
	if err := validateStudioDisabledLanguages(value); err != nil {
		return err
	}
	optionReloadMu.Lock()
	defer optionReloadMu.Unlock()

	option := Option{Key: studioDisabledLanguagesOption, Value: value}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&option).Error; err != nil {
		return err
	}
	config.OptionMapRWMutex.Lock()
	config.OptionMap[option.Key] = option.Value
	config.OptionMapRWMutex.Unlock()
	return nil
}
