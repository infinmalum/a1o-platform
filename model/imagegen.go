package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/infinmalum/a1o-platform/common/helper"
	"gorm.io/gorm"
)

// Image workbench (studio proposal §7): one ImageGeneration per submitted
// prompt, driven by a same-process worker over the database queue. Status
// distinguishes "upstream failed" (failed), "upstream succeeded but the
// products could not be saved" (save_failed — only the save may be retried,
// never a regeneration), and "upstream outcome unknown" (needs_reconciliation
// after a restart interrupted a running row).
const (
	ImageGenStatusQueued              = "queued"
	ImageGenStatusRunning             = "running"
	ImageGenStatusCompleted           = "completed"
	ImageGenStatusSaveFailed          = "save_failed"
	ImageGenStatusFailed              = "failed"
	ImageGenStatusCancelled           = "cancelled"
	ImageGenStatusNeedsReconciliation = "needs_reconciliation"
)

// ErrImageGenConflict reports a reused idempotency key with a different
// request fingerprint.
var ErrImageGenConflict = errors.New("图像生成幂等键已用于不同的请求")

type ImageGeneration struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"index;uniqueIndex:idx_imagegen_idem,priority:1"`
	Model     string `json:"model" gorm:"type:varchar(255)"`
	Prompt    string `json:"prompt" gorm:"type:text"`
	Size      string `json:"size" gorm:"type:varchar(32)"`
	Quality   string `json:"quality" gorm:"type:varchar(16)"`
	N         int    `json:"n"`
	Status    string `json:"status" gorm:"type:varchar(24);index"`
	Error     string `json:"error" gorm:"type:text"`
	SaveError string `json:"save_error" gorm:"type:text"`
	// Results holds the raw upstream data array JSON so retry-save can
	// re-attempt persistence without a second billed generation.
	Results        *string `json:"-" gorm:"type:text"`
	ConsumeLogId   int     `json:"consume_log_id" gorm:"default:0"`
	InputHash      string  `json:"-" gorm:"type:varchar(64)"`
	IdempotencyKey string  `json:"-" gorm:"type:varchar(255);uniqueIndex:idx_imagegen_idem,priority:2"`
	CreatedTime    int64   `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64   `json:"updated_time" gorm:"bigint"`
}

// ImageAsset is one persisted product of an ImageGeneration: metadata in the
// database, bytes in the media directory (never base64 in message text).
// Slot is the product's index in the upstream data array so a retry-save
// fills exactly the missing positions instead of duplicating.
type ImageAsset struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index"`
	RunId       int    `json:"run_id" gorm:"index"`
	Slot        int    `json:"slot"`
	Mime        string `json:"mime" gorm:"type:varchar(64)"`
	Size        int64  `json:"size" gorm:"bigint"`
	Path        string `json:"-" gorm:"type:varchar(255)"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

func imageGenInputHash(modelName, prompt, size, quality string, n int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", modelName, prompt, size, quality, n)))
	return hex.EncodeToString(digest[:])
}

// CreateImageGeneration inserts the queued row. A reused key with the same
// fingerprint returns the existing row (created=false); different content
// conflicts.
func CreateImageGeneration(gen *ImageGeneration) (existing *ImageGeneration, created bool, err error) {
	now := helper.GetTimestamp()
	gen.CreatedTime, gen.UpdatedTime = now, now
	gen.Status = ImageGenStatusQueued
	gen.InputHash = imageGenInputHash(gen.Model, gen.Prompt, gen.Size, gen.Quality, gen.N)
	if gen.IdempotencyKey == "" {
		return nil, false, errors.New("图像生成请求缺少幂等键")
	}
	var prior ImageGeneration
	lookup := DB.Where("user_id = ? AND idempotency_key = ?", gen.UserId, gen.IdempotencyKey).First(&prior)
	if lookup.Error == nil {
		if prior.InputHash != gen.InputHash {
			return nil, false, ErrImageGenConflict
		}
		return &prior, false, nil
	}
	if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return nil, false, lookup.Error
	}
	if err := DB.Create(gen).Error; err != nil {
		return nil, false, err
	}
	return gen, true, nil
}

// ClaimQueuedImageGeneration atomically moves one queued row to running so
// two workers on the same database cannot both execute it.
func ClaimQueuedImageGeneration() (*ImageGeneration, error) {
	for attempts := 0; attempts < 8; attempts++ {
		// Find (not First) keeps the empty-queue poll from logging a
		// not-found error on every beat, mirroring the work queue.
		var candidates []*ImageGeneration
		if err := DB.Where("status = ?", ImageGenStatusQueued).
			Order("id asc").Limit(1).Find(&candidates).Error; err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		candidate := candidates[0]
		claim := DB.Model(&ImageGeneration{Id: candidate.Id}).
			Where("status = ?", ImageGenStatusQueued).
			Updates(map[string]any{"status": ImageGenStatusRunning, "updated_time": helper.GetTimestamp()})
		if claim.Error != nil {
			return nil, claim.Error
		}
		if claim.RowsAffected == 1 {
			candidate.Status = ImageGenStatusRunning
			return candidate, nil
		}
		// Another worker claimed it between the read and the write; try the
		// next queued row.
	}
	return nil, nil
}

func GetImageGeneration(id, userId int) (*ImageGeneration, error) {
	var gen ImageGeneration
	if err := DB.Where("id = ? AND user_id = ?", id, userId).First(&gen).Error; err != nil {
		return nil, err
	}
	return &gen, nil
}

func GetUserImageGenerations(userId int, limit int) ([]*ImageGeneration, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []*ImageGeneration
	err := DB.Where("user_id = ?", userId).Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// CountActiveImageGenerations counts the caller's queued+running rows for
// the per-user concurrency cap.
func CountActiveImageGenerations(userId int) (int64, error) {
	var count int64
	err := DB.Model(&ImageGeneration{}).Where("user_id = ? AND status IN ?", userId,
		[]string{ImageGenStatusQueued, ImageGenStatusRunning}).Count(&count).Error
	return count, err
}

// CancelQueuedImageGeneration cancels a row that has not been claimed yet;
// a running row cannot be cancelled because the upstream call cannot be
// withdrawn.
func CancelQueuedImageGeneration(id, userId int) error {
	claim := DB.Model(&ImageGeneration{Id: id}).
		Where("user_id = ? AND status = ?", userId, ImageGenStatusQueued).
		Updates(map[string]any{"status": ImageGenStatusCancelled, "updated_time": helper.GetTimestamp()})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return errors.New("仅排队中的生成可以取消；已提交上游的请求无法撤回")
	}
	return nil
}

func completeImageGeneration(id int, values map[string]any) error {
	values["updated_time"] = helper.GetTimestamp()
	return DB.Model(&ImageGeneration{Id: id}).Updates(values).Error
}

// CompleteImageGeneration records a fully succeeded generation.
func CompleteImageGeneration(id int, results string, consumeLogId int) error {
	return completeImageGeneration(id, map[string]any{
		"status": ImageGenStatusCompleted, "results": results,
		"consume_log_id": consumeLogId, "error": "", "save_error": "",
	})
}

// MarkImageGenerationSaveFailed records that the upstream succeeded and was
// charged, but the products could not be persisted; retry-save may proceed.
func MarkImageGenerationSaveFailed(id int, results, saveError string, consumeLogId int) error {
	return completeImageGeneration(id, map[string]any{
		"status": ImageGenStatusSaveFailed, "results": results,
		"consume_log_id": consumeLogId, "save_error": saveError,
	})
}

func FailImageGeneration(id int, message string) error {
	return completeImageGeneration(id, map[string]any{
		"status": ImageGenStatusFailed, "error": message,
	})
}

// ReconcileStuckImageGenerations moves rows left running by a crashed
// process into needs_reconciliation: the upstream may have accepted and
// billed the call, so it is never silently retried.
func ReconcileStuckImageGenerations() (int64, error) {
	claim := DB.Model(&ImageGeneration{}).
		Where("status = ?", ImageGenStatusRunning).
		Updates(map[string]any{"status": ImageGenStatusNeedsReconciliation, "updated_time": helper.GetTimestamp()})
	return claim.RowsAffected, claim.Error
}

func CreateImageAsset(asset *ImageAsset) error {
	asset.CreatedTime = helper.GetTimestamp()
	return DB.Create(asset).Error
}

func GetRunImageAssetSlots(runId, userId int) (map[int]bool, error) {
	var assets []*ImageAsset
	if err := DB.Where("run_id = ? AND user_id = ?", runId, userId).Find(&assets).Error; err != nil {
		return nil, err
	}
	slots := make(map[int]bool, len(assets))
	for _, asset := range assets {
		slots[asset.Slot] = true
	}
	return slots, nil
}

// SaveImageAssetPayload persists one product's bytes into the media
// directory: the row is created first so the file name can carry its id,
// and a failed write removes the row again instead of leaving a phantom.
func SaveImageAssetPayload(userId, runId, slot int, payload []byte) (*ImageAsset, error) {
	asset := &ImageAsset{
		UserId: userId, RunId: runId, Slot: slot,
		Mime: http.DetectContentType(payload),
		Size: int64(len(payload)),
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(asset).Error; err != nil {
			return err
		}
		dir := imageAssetRoot()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		path := filepath.Join(dir, fmt.Sprintf("%d_%d", userId, asset.Id))
		if err := os.WriteFile(path, payload, 0600); err != nil {
			return err
		}
		asset.Path = path
		return tx.Model(asset).Update("path", asset.Path).Error
	})
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func imageAssetRoot() string {
	return filepath.Join("data", "image-assets")
}

// ReadImageAssetPayload loads one product's bytes for owner-checked serving.
func ReadImageAssetPayload(asset *ImageAsset) ([]byte, error) {
	return os.ReadFile(asset.Path)
}

func GetImageAsset(id, userId int) (*ImageAsset, error) {
	var asset ImageAsset
	if err := DB.Where("id = ? AND user_id = ?", id, userId).First(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func GetRunImageAssets(runId, userId int) ([]*ImageAsset, error) {
	var assets []*ImageAsset
	err := DB.Where("run_id = ? AND user_id = ?", runId, userId).Order("id asc").Find(&assets).Error
	return assets, err
}

// GetUserImageAssets lists the most recent products for the gallery, newest
// first.
func GetUserImageAssets(userId int, limit int) ([]*ImageAsset, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	var assets []*ImageAsset
	err := DB.Where("user_id = ?", userId).Order("id desc").Limit(limit).Find(&assets).Error
	return assets, err
}
