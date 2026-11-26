package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/common/random"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const privacyRetention = 15 * 24 * time.Hour

func privacyDeadline(now time.Time) int64 {
	// Round up so Unix-second precision cannot shorten the retention window.
	return now.Add(privacyRetention).Unix() + 1
}

func markAccountDeleted(ctx context.Context, userID int) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deadline := privacyDeadline(time.Now())
		revoked := random.GetUUID()
		result := tx.Model(&User{}).Where("id = ? AND role < ? AND status != ?", userID, RoleRootUser, UserStatusDeleted).
			Updates(map[string]any{
				"status": UserStatusDeleted, "privacy_purge_at": deadline, "memory_enabled": false,
				"username": "deleted_" + revoked, "access_token": random.GetUUID(), "aff_code": random.GetUUID(),
				"display_name": "", "email": "", "password": "", "github_id": "", "lark_id": "", "oidc_id": "", "inviter_id": 0,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var user User
			if err := tx.Select("status").Take(&user, userID).Error; err != nil {
				return err
			}
			if user.Status == UserStatusDeleted {
				return nil
			}
			return errors.New("account cannot be deleted")
		}
		if err := tx.Where("user_id = ?", userID).Delete(&Token{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ChatSession{}).Where("user_id = ? AND privacy_purge_at = 0", userID).
			Update("privacy_purge_at", deadline).Error; err != nil {
			return err
		}
		if err := tx.Model(&AgentRun{}).Where("user_id = ? AND kind = ? AND status IN ?", userID, "work",
			[]string{RunStatusQueued, RunStatusRunning, RunStatusWaitingInput, RunStatusNeedsReconciliation, RunStatusWaitingApproval}).
			Updates(map[string]any{"status": RunStatusCancelled, "lease_expire_time": 0, "updated_time": time.Now().Unix()}).Error; err != nil {
			return err
		}
		return tx.Model(&ImageGeneration{}).Where("user_id = ? AND status = ?", userID, ImageGenStatusQueued).
			Update("status", ImageGenStatusCancelled).Error
	})
}

type PrivacyReviewAccess struct {
	Id        int    `json:"id"`
	Reviewer  int    `json:"reviewer_id"`
	SessionId int    `json:"session_id" gorm:"index"`
	Reason    string `json:"reason" gorm:"size:300"`
	Accessed  int64  `json:"accessed_at" gorm:"index"`
}

type RetainedChat struct {
	Id      int   `json:"id"`
	UserId  int   `json:"user_id"`
	PurgeAt int64 `json:"purge_at" gorm:"column:privacy_purge_at"`
}

type ReviewMessage struct {
	Id          int    `json:"id"`
	Role        string `json:"role"`
	Content     string `json:"content"`
	CreatedTime int64  `json:"created_time"`
}

const ReviewPageSize = 25

func ListRetainedChats(ctx context.Context, before int) ([]RetainedChat, error) {
	chats := []RetainedChat{}
	query := DB.WithContext(ctx).Model(&ChatSession{}).Select("id", "user_id", "privacy_purge_at").
		Where("privacy_purge_at > ?", time.Now().Unix()).Order("id DESC").Limit(ReviewPageSize + 1)
	if before > 0 {
		query = query.Where("id < ?", before)
	}
	err := query.Find(&chats).Error
	return chats, err
}

func ListPrivacyReviewAccess(ctx context.Context, before int) ([]PrivacyReviewAccess, error) {
	rows := []PrivacyReviewAccess{}
	query := DB.WithContext(ctx).Order("id DESC").Limit(ReviewPageSize + 1)
	if before > 0 {
		query = query.Where("id < ?", before)
	}
	err := query.Find(&rows).Error
	return rows, err
}

func ReviewRetainedChat(ctx context.Context, reviewer, sessionID, after int, reason string) ([]ReviewMessage, error) {
	messages := []ReviewMessage{}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().Unix()
		var session ChatSession
		if err := tx.Select("id", "privacy_purge_at").Where("id = ? AND privacy_purge_at > ?", sessionID, now).Take(&session).Error; err != nil {
			return err
		}
		if err := tx.Create(&PrivacyReviewAccess{
			Reviewer: reviewer, SessionId: sessionID, Reason: reason, Accessed: now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ChatMessage{}).Select("id", "role", "content", "created_time").
			Where("session_id = ? AND id > ? AND role IN ? AND content <> ''", sessionID, after, []string{ChatRoleUser, ChatRoleAssistant}).
			Order("id ASC").Limit(ReviewPageSize + 1).Find(&messages).Error; err != nil {
			return err
		}
		if session.PrivacyPurgeAt <= time.Now().Unix() {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	return messages, err
}

// Runs replicate visible messages and can hold raw prompt or tool data too.
func purgeRunData(tx *gorm.DB, filter string, id int) error {
	runs := tx.Model(&AgentRun{}).Select("id").Where(filter, id)
	for _, row := range []any{&RunInput{}, &ToolExecution{}, &ModelInvocation{}, &RunEvent{}} {
		if err := tx.Where("run_id IN (?)", runs).Delete(row).Error; err != nil {
			return err
		}
	}
	return tx.Where(filter, id).Delete(&AgentRun{}).Error
}

func purgeRetainedChat(ctx context.Context, sessionID int, now int64) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		session := ChatSession{}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id = ? AND privacy_purge_at BETWEEN 1 AND ?", sessionID, now).Take(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := purgeRunData(tx, "session_id = ?", sessionID); err != nil {
			return err
		}
		if err := tx.Where("session_id = ?", sessionID).Delete(&ChatMessage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&session).Error
	})
}

func storedFilename(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) || filepath.Base(rel) != rel {
		return "", errors.New("invalid stored file path")
	}
	return rel, nil
}

func deletePrivacyFile(rootPath, path string, thumbnail bool) error {
	if path == "" {
		return nil
	}
	name, err := storedFilename(rootPath, path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if thumbnail {
		if err := root.Remove(name + ".thumb.jpg"); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func purgeDeletedAccount(ctx context.Context, userID int, now int64) error {
	// The log database can be separate. Scrub it first so any failure can retry.
	logDB := LOG_DB.WithContext(ctx)
	if err := logDB.Where("user_id = ? AND type NOT IN ?", userID, []int{LogTypeTopup, LogTypeConsume}).Delete(&Log{}).Error; err != nil {
		return err
	}
	if err := logDB.Model(&Log{}).Where("user_id = ?", userID).
		Updates(map[string]any{"username": "", "token_name": "", "content": "", "request_id": ""}).Error; err != nil {
		return err
	}

	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND status = ? AND privacy_purge_at BETWEEN 1 AND ?",
			userID, UserStatusDeleted, now).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var files []ChatFile
		if err := tx.Where("user_id = ?", userID).FindInBatches(&files, 50, func(_ *gorm.DB, _ int) error {
			for _, file := range files {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := deletePrivacyFile(chatFileRoot(), file.Path, false); err != nil {
					return err
				}
			}
			return nil
		}).Error; err != nil {
			return err
		}
		var media []ImageAsset
		if err := tx.Where("user_id = ?", userID).FindInBatches(&media, 50, func(_ *gorm.DB, _ int) error {
			for _, asset := range media {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := deletePrivacyFile(imageAssetRoot(), asset.Path, true); err != nil {
					return err
				}
			}
			return nil
		}).Error; err != nil {
			return err
		}
		if err := os.RemoveAll(UserMemoryStore(userID).Dir); err != nil {
			return err
		}

		if err := purgeRunData(tx, "user_id = ?", userID); err != nil {
			return err
		}
		libraries := tx.Model(&KnowledgeLibrary{}).Select("id").Where("user_id = ?", userID)
		for _, row := range []any{&KnowledgeChunk{}, &KnowledgeDocument{}} {
			if err := tx.Where("library_id IN (?)", libraries).Delete(row).Error; err != nil {
				return err
			}
		}
		definitions := tx.Model(&AgentDefinition{}).Select("id").Where("owner_id = ?", userID)
		if err := tx.Where("definition_id IN (?)", definitions).Delete(&AgentVersion{}).Error; err != nil {
			return err
		}
		sessions := tx.Model(&ChatSession{}).Select("id").Where("user_id = ?", userID)
		if err := tx.Where("session_id IN (?)", sessions).Delete(&ChatMessage{}).Error; err != nil {
			return err
		}
		for _, row := range []any{&ChatSession{}, &ChatFile{}, &KnowledgeLibrary{}, &WorkTask{}, &RunInput{}, &ImageAsset{}, &ImageGeneration{}, &Token{}} {
			if err := tx.Where("user_id = ?", userID).Delete(row).Error; err != nil {
				return err
			}
		}
		for _, row := range []any{&AgentDefinition{}, &ChatMCPServer{}, &ChatSkill{}} {
			if err := tx.Where("owner_id = ?", userID).Delete(row).Error; err != nil {
				return err
			}
		}
		// Keep the pseudonymous account ID so financial records and late
		// provider callbacks still have their original settlement reference.
		return tx.Model(&user).Update("privacy_purge_at", 0).Error
	})
}

type privacyPruner struct {
	chatCursor    int
	accountCursor int
}

func (p *privacyPruner) prune(ctx context.Context, now int64) error {
	chats := DB.Model(&ChatSession{})
	accounts := DB.Model(&User{}).Where("status = ?", UserStatusDeleted)
	chatErr := prunePrivacyBatch(ctx, chats, &p.chatCursor, 100, now, "conversation", purgeRetainedChat)
	accountErr := prunePrivacyBatch(ctx, accounts, &p.accountCursor, 25, now, "account", purgeDeletedAccount)
	auditCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	auditErr := DB.WithContext(auditCtx).Where("accessed < ?", now-int64((90*24*time.Hour)/time.Second)).
		Delete(&PrivacyReviewAccess{}).Error
	return errors.Join(chatErr, accountErr, auditErr)
}

func prunePrivacyBatch(ctx context.Context, query *gorm.DB, cursor *int, limit int, now int64,
	kind string, purge func(context.Context, int, int64) error,
) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var expired []int
	if err := query.WithContext(ctx).Where("privacy_purge_at BETWEEN 1 AND ? AND id > ?", now, *cursor).
		Order("id").Limit(limit).Pluck("id", &expired).Error; err != nil {
		return err
	}
	var failed []error
	for _, id := range expired {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failed, err)...)
		}
		// Finish scanning other records before retrying an item that blocked this pass.
		*cursor = id
		if err := purge(ctx, id, now); err != nil {
			failed = append(failed, fmt.Errorf("%s %d: %w", kind, id, err))
		}
	}
	if len(expired) < limit {
		*cursor = 0
	}
	return errors.Join(failed...)
}

func PrivacyRetentionLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var pruner privacyPruner
	for {
		if err := pruner.prune(context.Background(), time.Now().Unix()); err != nil {
			logger.SysError("privacy cleanup: " + strings.ReplaceAll(err.Error(), "\n", "; "))
		}
		<-ticker.C
	}
}
