package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/infinmalum/a1o-platform/common/helper"
	"gorm.io/gorm"
)

// Translation workbench (studio proposal §6): one record per submitted
// translation, persisted before the upstream call so a retried click with
// the same idempotency key replays the stored result instead of paying
// twice. Engine is "deepl" (character billing via the DeepL channel) or
// "llm" (a user-selected model billed as a normal token call).
const (
	TranslationEngineDeepL = "deepl"
	TranslationEngineLLM   = "llm"

	TranslationStatusRunning   = "running"
	TranslationStatusCompleted = "completed"
	TranslationStatusFailed    = "failed"
)

// ErrTranslationConflict reports a reused idempotency key carrying a
// different request; the same key with the same request is an idempotent hit.
var ErrTranslationConflict = errors.New("翻译幂等键已用于不同的翻译请求")

type TranslationRecord struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id" gorm:"index;uniqueIndex:idx_translation_idem,priority:1"`
	Engine       string `json:"engine" gorm:"type:varchar(16)"`
	Model        string `json:"model" gorm:"type:varchar(255)"`
	SourceLang   string `json:"source_lang" gorm:"type:varchar(16)"`
	TargetLang   string `json:"target_lang" gorm:"type:varchar(16)"`
	InputText    string `json:"input_text" gorm:"type:text"`
	OutputText   string `json:"output_text" gorm:"type:text"`
	DetectedLang string `json:"detected_lang" gorm:"type:varchar(16)"`
	// BilledCharacters carries the DeepL authoritative billed_characters;
	// LLM engine leaves it 0 and reports token usage instead.
	BilledCharacters int    `json:"billed_characters" gorm:"default:0"`
	InputTokens      int    `json:"input_tokens" gorm:"default:0"`
	OutputTokens     int    `json:"output_tokens" gorm:"default:0"`
	ConsumeLogId     int    `json:"consume_log_id" gorm:"default:0"`
	Status           string `json:"status" gorm:"type:varchar(16);index"`
	Error            string `json:"error" gorm:"type:text"`
	// InputHash fingerprints the request content so a key replay with
	// different text conflicts instead of silently returning stale output.
	InputHash      string `json:"-" gorm:"type:varchar(64)"`
	IdempotencyKey string `json:"-" gorm:"type:varchar(255);uniqueIndex:idx_translation_idem,priority:2"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64  `json:"updated_time" gorm:"bigint"`
}

func translationInputHash(engine, targetLang, text string) string {
	digest := sha256.Sum256([]byte(engine + "\x00" + targetLang + "\x00" + text))
	return hex.EncodeToString(digest[:])
}

// CreateTranslationRecord inserts the running row. A reused key with the
// same fingerprint returns the existing record (created=false); a reused key
// with different content conflicts.
func CreateTranslationRecord(record *TranslationRecord) (existing *TranslationRecord, created bool, err error) {
	now := helper.GetTimestamp()
	record.CreatedTime, record.UpdatedTime = now, now
	record.InputHash = translationInputHash(record.Engine, record.TargetLang, record.InputText)
	if record.IdempotencyKey == "" {
		return nil, false, errors.New("翻译请求缺少幂等键")
	}
	var prior TranslationRecord
	lookup := DB.Where("user_id = ? AND idempotency_key = ?", record.UserId, record.IdempotencyKey).First(&prior)
	if lookup.Error == nil {
		if prior.InputHash != record.InputHash {
			return nil, false, ErrTranslationConflict
		}
		return &prior, false, nil
	}
	if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return nil, false, lookup.Error
	}
	if err := DB.Create(record).Error; err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func CompleteTranslationRecord(id int, output, detectedLang string, billed, inputTokens, outputTokens, consumeLogId int) error {
	return DB.Model(&TranslationRecord{Id: id}).Updates(map[string]any{
		"status": TranslationStatusCompleted, "output_text": output,
		"detected_lang": detectedLang, "billed_characters": billed,
		"input_tokens": inputTokens, "output_tokens": outputTokens,
		"consume_log_id": consumeLogId, "error": "",
		"updated_time": helper.GetTimestamp(),
	}).Error
}

func FailTranslationRecord(id int, message string) error {
	return DB.Model(&TranslationRecord{Id: id}).Updates(map[string]any{
		"status": TranslationStatusFailed, "error": message,
		"updated_time": helper.GetTimestamp(),
	}).Error
}

func GetUserTranslations(userId int, limit int) ([]*TranslationRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var records []*TranslationRecord
	err := DB.Where("user_id = ?", userId).Order("id desc").Limit(limit).Find(&records).Error
	return records, err
}

func GetTranslationRecordById(id, userId int) (*TranslationRecord, error) {
	var record TranslationRecord
	if err := DB.Where("id = ? AND user_id = ?", id, userId).First(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}
