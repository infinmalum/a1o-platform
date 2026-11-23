package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/infinmalum/a1o-platform/common/helper"
	"gorm.io/gorm"
)

func ReserveArkMedia(ctx context.Context, gen *ImageGeneration, channelID int, quota int64) error {
	if quota <= 0 {
		return errors.New("invalid media quota")
	}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(gen).Where("status = ? AND ark_quota = 0", ImageGenStatusRunning).
			Updates(map[string]any{"ark_channel_id": channelID, "ark_quota": quota})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New("media job already reserved")
		}
		return decreaseUserQuota(tx, gen.UserId, quota)
	})
	if err == nil {
		gen.ArkChannelID, gen.ArkQuota = channelID, quota
	}
	return err
}

func StoreArkVideoTask(gen *ImageGeneration, taskID string) error {
	return DB.Model(gen).Update("ark_task_id", taskID).Error
}

func ClaimArkVideoPoll() (*ImageGeneration, error) {
	now := helper.GetTimestamp()
	var jobs []ImageGeneration
	if err := DB.Where("status = ? AND ark_task_id <> ? AND ark_next_poll <= ?", ImageGenStatusRunning, "", now).
		Order("ark_next_poll asc, id asc").Limit(1).Find(&jobs).Error; err != nil || len(jobs) == 0 {
		return nil, err
	}
	job := &jobs[0]
	lease := DB.Model(job).Where("status = ? AND ark_next_poll <= ?", ImageGenStatusRunning, now).
		UpdateColumn("ark_next_poll", now+int64((3*time.Minute)/time.Second))
	if lease.Error != nil || lease.RowsAffected != 1 {
		return nil, lease.Error
	}
	return job, nil
}

func ScheduleArkVideoPoll(gen *ImageGeneration, delay time.Duration) error {
	return DB.Model(gen).UpdateColumn("ark_next_poll", helper.GetTimestamp()+int64(delay/time.Second)).Error
}

// FinishArkMedia applies the terminal state and the quota change atomically.
func FinishArkMedia(ctx context.Context, gen *ImageGeneration, status, message string, results *string) (bool, error) {
	var finished bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&ImageGeneration{Id: gen.Id}).Where("status = ? AND ark_quota > 0", ImageGenStatusRunning).Updates(map[string]any{
			"status": status, "ark_quota": 0, "updated_time": helper.GetTimestamp(),
			"results": results, "error": message, "save_error": message,
		})
		if update.Error != nil || update.RowsAffected == 0 {
			return update.Error
		}
		finished = true
		user := tx.Model(&User{}).Where("id = ?", gen.UserId)
		if status == ImageGenStatusFailed {
			return user.UpdateColumn("quota", gorm.Expr("quota + ?", gen.ArkQuota)).Error
		}
		if err := user.UpdateColumns(map[string]any{
			"used_quota":    gorm.Expr("used_quota + ?", gen.ArkQuota),
			"request_count": gorm.Expr("request_count + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&Channel{}).Where("id = ?", gen.ArkChannelID).
			UpdateColumn("used_quota", gorm.Expr("used_quota + ?", gen.ArkQuota)).Error
	})
	return finished && err == nil, err
}

func SaveArkVideoAsset(gen *ImageGeneration, body io.Reader, limit int64) error {
	existing, err := GetRunImageAssetSlots(gen.Id, gen.UserId)
	if err != nil || existing[0] {
		return err
	}
	var header [512]byte
	n, err := io.ReadFull(body, header[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	if http.DetectContentType(header[:n]) != "video/mp4" {
		return errors.New("Ark result is not MP4")
	}
	dir := imageAssetRoot()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "ark-video-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(header[:n]); err != nil {
		return err
	}
	copied, err := io.Copy(file, io.LimitReader(body, limit-int64(n)+1))
	if err != nil {
		return err
	}
	size := int64(n) + copied
	if size > limit {
		return errors.New("Ark video exceeds download limit")
	}
	if err := file.Close(); err != nil {
		return err
	}
	asset := &ImageAsset{UserId: gen.UserId, RunId: gen.Id, Mime: "video/mp4", Size: size, CreatedTime: helper.GetTimestamp()}
	path := file.Name() + ".mp4"
	err = DB.Transaction(func(tx *gorm.DB) error {
		asset.Path = path
		if err := tx.Create(asset).Error; err != nil {
			return err
		}
		return os.Rename(file.Name(), path)
	})
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func MarkArkMediaUncertain(id int) error {
	return completeImageGeneration(id, map[string]any{
		"status": ImageGenStatusNeedsReconciliation,
		"error":  "Ark generation outcome needs reconciliation",
	})
}
