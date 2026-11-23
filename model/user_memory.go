package model

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/infinmalum/Lapua/memory"
	"github.com/infinmalum/a1o-platform/common/config"
	"gorm.io/gorm"
)

var ErrMemoryDisabled = errors.New("long-term memory is disabled")

type UserMemory struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Type        memory.Type `json:"type"`
	Content     string      `json:"content"`
	Modified    time.Time   `json:"modified_at"`
}

func UserMemoryStore(userID int) *memory.Store {
	return memory.NewStore(filepath.Join("data", "memory", "users", strconv.Itoa(userID)))
}

func MemoryEnabled(ctx context.Context, userID int) bool {
	var user User
	return config.ChatMemoryEnabled && userID > 0 && DB != nil &&
		DB.WithContext(ctx).Select("status", "memory_enabled").Take(&user, userID).Error == nil &&
		user.Status == UserStatusEnabled && user.MemoryEnabled
}

func SetUserMemory(ctx context.Context, userID int, enabled bool) error {
	return DB.WithContext(ctx).Model(&User{}).Where("id = ? AND status = ?", userID, UserStatusEnabled).
		Update("memory_enabled", enabled).Error
}

// The no-op UPDATE locks the owner row even on SQLite. All memory tools
// and removals go through this lock so an older tool cannot undo a deletion.
func withUserMemory(ctx context.Context, userID int, requireOptIn bool, operation func(*memory.Store, bool) error) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", userID).UpdateColumn("memory_enabled", gorm.Expr("memory_enabled")).Error; err != nil {
			return err
		}
		var user User
		if err := tx.Select("status", "memory_enabled").Where("status = ?", UserStatusEnabled).Take(&user, userID).Error; err != nil {
			return err
		}
		if requireOptIn && (!user.MemoryEnabled || !config.ChatMemoryEnabled) {
			return ErrMemoryDisabled
		}
		return operation(UserMemoryStore(userID), user.MemoryEnabled)
	})
}

func WithEnabledMemory(ctx context.Context, userID int, operation func() error) error {
	return withUserMemory(ctx, userID, true, func(_ *memory.Store, _ bool) error { return operation() })
}

func ListUserMemory(ctx context.Context, userID int) (bool, []UserMemory, error) {
	entries := []UserMemory{}
	var enabled bool
	err := withUserMemory(ctx, userID, false, func(store *memory.Store, preference bool) error {
		enabled = preference
		items, err := store.Scan()
		if err != nil {
			return err
		}
		for _, item := range items {
			entries = append(entries, UserMemory{
				ID: filepath.Base(item.Path), Name: item.Name, Description: item.Description,
				Type: item.Type, Content: item.Content, Modified: item.ModTime,
			})
		}
		return nil
	})
	return enabled, entries, err
}

func DeleteUserMemory(ctx context.Context, userID int, entryID string) error {
	return withUserMemory(ctx, userID, false, func(store *memory.Store, _ bool) error {
		if entryID == "" {
			return os.RemoveAll(store.Dir)
		}
		items, err := store.Scan()
		if err != nil {
			return err
		}
		for _, item := range items {
			if filepath.Base(item.Path) != entryID {
				continue
			}
			// Remove the old index first so deleted facts cannot be left in it.
			if err := os.Remove(filepath.Join(store.Dir, "MEMORY.md")); err != nil && !os.IsNotExist(err) {
				return err
			}
			return os.Remove(item.Path)
		}
		return gorm.ErrRecordNotFound
	})
}
