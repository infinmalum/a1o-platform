package chat

import (
	"crypto/rand"
	"encoding/hex"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/one-gateway/model"
)

// openChatTestDB opens an isolated in-memory SQLite database with the chat
// runtime tables.
func openChatTestDB() (*gorm.DB, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open("file:chat-"+hex.EncodeToString(buf)+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.ChatSession{}, &model.ChatMessage{}, &model.ChatFile{},
		&model.ChatSkill{}, &model.ChatMCPServer{}, &model.Token{}, &model.User{}); err != nil {
		return nil, err
	}
	return db, nil
}
