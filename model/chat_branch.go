package model

import (
	"context"
	"errors"

	"github.com/infinmalum/a1o-platform/common/helper"
	"gorm.io/gorm"
)

var ErrChatBranchUnavailable = errors.New("只能从当前会话的最后一条用户消息重新生成")

// BranchChatSession copies the history through the latest user message. The
// original messages, replies, runs, and attachment records remain intact.
func BranchChatSession(ctx context.Context, session *ChatSession, messageID int, title string) (*ChatSession, int, error) {
	branch := *session
	branch.Id = 0
	branch.Title = title
	branch.CompactedThrough = 0
	branch.CreatedTime = helper.GetTimestamp()
	branch.UpdatedTime = branch.CreatedTime
	if session.CompactedThrough >= messageID {
		branch.Summary = ""
	}

	var copiedMessageID int
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var latest ChatMessage
		res := tx.Select("id").Where("session_id = ? AND role = ?", session.Id, ChatRoleUser).Order("id DESC").Take(&latest)
		if errors.Is(res.Error, gorm.ErrRecordNotFound) || (res.Error == nil && latest.Id != messageID) {
			return ErrChatBranchUnavailable
		}
		if res.Error != nil {
			return res.Error
		}
		if err := tx.Create(&branch).Error; err != nil {
			return err
		}
		var messages []ChatMessage
		err := tx.Where("session_id = ? AND id <= ?", session.Id, messageID).
			FindInBatches(&messages, 40, func(_ *gorm.DB, _ int) error {
				for _, original := range messages {
					copy := original
					copy.Id = 0
					copy.SessionId = branch.Id
					if err := tx.Create(&copy).Error; err != nil {
						return err
					}
					if original.Id == messageID {
						copiedMessageID = copy.Id
					}
					if original.Id == session.CompactedThrough && branch.Summary != "" {
						branch.CompactedThrough = copy.Id
					}
				}
				return nil
			}).Error
		if err != nil {
			return err
		}
		if copiedMessageID == 0 {
			return ErrChatBranchUnavailable
		}
		if branch.CompactedThrough > 0 {
			return tx.Model(&branch).Update("compacted_through", branch.CompactedThrough).Error
		}
		return nil
	})
	return &branch, copiedMessageID, err
}

func GetLatestChatMessage(sessionID int) (*ChatMessage, error) {
	var message ChatMessage
	err := DB.Where("session_id = ?", sessionID).Order("id DESC").Take(&message).Error
	return &message, err
}
