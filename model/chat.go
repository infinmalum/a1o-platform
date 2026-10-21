package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"github.com/infinmalum/one-gateway/common/helper"
	"github.com/infinmalum/one-gateway/common/random"
)

// Tables backing the built-in chat runtime: sessions hold per-conversation
// configuration and the running summary, messages hold the persisted history
// (including intermediate tool-call turns), files are attachments kept on disk
// with metadata rows, skills are prompt fragments with tool bundles, and MCP
// servers describe tool providers (owner 0 = administrator-managed global).

const (
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
	ChatRoleTool      = "tool"

	ChatFileKindText  = "text"
	ChatFileKindImage = "image"

	// ChatTokenName marks the hidden per-user token that carries billing for
	// the built-in chat so usage lands in the normal consume logs.
	ChatTokenName = "内置聊天"

	ChatMCPServerStdio = "stdio"
	ChatMCPServerHTTP  = "http"

	ChatSkillOwnerGlobal = 0
)

type ChatSession struct {
	Id               int      `json:"id"`
	UserId           int      `json:"user_id" gorm:"index"`
	Title            string   `json:"title" gorm:"type:varchar(255)"`
	Model            string   `json:"model" gorm:"type:varchar(255)"`
	SystemPrompt     string   `json:"system_prompt" gorm:"type:text"`
	Temperature      *float64 `json:"temperature"`
	TopP             *float64 `json:"top_p"`
	MaxTokens        int      `json:"max_tokens"`
	SandboxEnabled   bool     `json:"sandbox_enabled" gorm:"default:false"`
	ContextBudget    int      `json:"context_budget"` // 0 means the global default
	SkillIds         *string  `json:"skill_ids" gorm:"type:text"`
	Summary          string   `json:"summary" gorm:"type:text"`
	CompactedThrough int      `json:"compacted_through"`
	CreatedTime      int64    `json:"created_time" gorm:"bigint"`
	UpdatedTime      int64    `json:"updated_time" gorm:"bigint"`
}

type ChatMessage struct {
	Id        int    `json:"id"`
	SessionId int    `json:"session_id" gorm:"index"`
	Role      string `json:"role" gorm:"type:varchar(16)"`
	Content   string `json:"content" gorm:"type:text"`
	// Parts holds JSON attachments for user messages as []ChatMessagePart;
	// payloads resolve from ChatFile at build time.
	Parts       *string `json:"parts" gorm:"type:text"`
	ToolCalls   *string `json:"tool_calls" gorm:"type:text"`
	ToolCallId  string  `json:"tool_call_id" gorm:"type:varchar(255)"`
	Name        string  `json:"name" gorm:"type:varchar(255)"`
	Tokens      int     `json:"tokens"`
	CreatedTime int64   `json:"created_time" gorm:"bigint"`
}

// ChatMessagePart is one attachment reference stored in ChatMessage.Parts.
type ChatMessagePart struct {
	Type   string `json:"type"`
	FileID int    `json:"file_id"`
}

type ChatFile struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index"`
	Name        string `json:"name" gorm:"type:varchar(255)"`
	Kind        string `json:"kind" gorm:"type:varchar(16)"`
	Size        int64  `json:"size" gorm:"bigint"`
	Path        string `json:"path" gorm:"type:varchar(255)"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

type ChatSkill struct {
	Id           int    `json:"id"`
	OwnerId      int    `json:"owner_id" gorm:"index;uniqueIndex:idx_chat_skill_owner_name"`
	Name         string `json:"name" gorm:"type:varchar(255);uniqueIndex:idx_chat_skill_owner_name"`
	Description  string `json:"description" gorm:"type:varchar(1024)"`
	Instructions string `json:"instructions" gorm:"type:text"`
	// Tools is a JSON array of tool names the skill enables; the built-in
	// sandbox tool is named "sandbox", MCP tools use their virtual names.
	Tools       *string `json:"tools" gorm:"type:text"`
	Enabled     bool    `json:"enabled" gorm:"default:true"`
	CreatedTime int64   `json:"created_time" gorm:"bigint"`
	UpdatedTime int64   `json:"updated_time" gorm:"bigint"`
}

type ChatMCPServer struct {
	Id          int     `json:"id"`
	OwnerId     int     `json:"owner_id" gorm:"index"`
	Name        string  `json:"name" gorm:"type:varchar(255)"`
	Transport   string  `json:"transport" gorm:"type:varchar(16)"`
	Command     string  `json:"command" gorm:"type:varchar(1024)"`
	Args        *string `json:"args" gorm:"type:text"`
	Env         *string `json:"env" gorm:"type:text"`
	URL         string  `json:"url" gorm:"type:varchar(1024)"`
	Headers     *string `json:"headers" gorm:"type:text"`
	Enabled     bool    `json:"enabled" gorm:"default:true"`
	CreatedTime int64   `json:"created_time" gorm:"bigint"`
	UpdatedTime int64   `json:"updated_time" gorm:"bigint"`
}

// decodeJSONColumn decodes one stored JSON column; empty and malformed
// values degrade to the zero value instead of failing the reader.
func decodeJSONColumn[T any](raw *string) (decoded T) {
	if raw == nil || *raw == "" {
		return
	}
	_ = json.Unmarshal([]byte(*raw), &decoded)
	return
}

// ChatSkillView is the API projection of ChatSkill with the stored tool list
// decoded, so clients receive a real array instead of a JSON-encoded string.
type ChatSkillView struct {
	Id           int      `json:"id"`
	OwnerId      int      `json:"owner_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
	Enabled      bool     `json:"enabled"`
}

func (s *ChatSkill) View() ChatSkillView {
	return ChatSkillView{
		Id:           s.Id,
		OwnerId:      s.OwnerId,
		Name:         s.Name,
		Description:  s.Description,
		Instructions: s.Instructions,
		Tools:        decodeJSONColumn[[]string](s.Tools),
		Enabled:      s.Enabled,
	}
}

// ChatMCPServerView is the API projection of ChatMCPServer with the stored
// Args/Env/Headers JSON columns decoded into real values.
type ChatMCPServerView struct {
	Id        int               `json:"id"`
	OwnerId   int               `json:"owner_id"`
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Enabled   bool              `json:"enabled"`
}

func (s *ChatMCPServer) View() ChatMCPServerView {
	return ChatMCPServerView{
		Id:        s.Id,
		OwnerId:   s.OwnerId,
		Name:      s.Name,
		Transport: s.Transport,
		Command:   s.Command,
		Args:      decodeJSONColumn[[]string](s.Args),
		Env:       decodeJSONColumn[map[string]string](s.Env),
		URL:       s.URL,
		Headers:   decodeJSONColumn[map[string]string](s.Headers),
		Enabled:   s.Enabled,
	}
}

func (s *ChatSession) SkillIdList() []int {
	if s.SkillIds == nil || *s.SkillIds == "" {
		return nil
	}
	var ids []int
	if err := json.Unmarshal([]byte(*s.SkillIds), &ids); err != nil {
		return nil
	}
	return ids
}

func (s *ChatSession) SetSkillIdList(ids []int) error {
	if len(ids) == 0 {
		s.SkillIds = nil
		return nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	encoded := string(raw)
	s.SkillIds = &encoded
	return nil
}

// ToolList decodes the skill's bound tool names; nil means the skill binds
// no tools and therefore narrows nothing.
func (s *ChatSkill) ToolList() []string {
	if s.Tools == nil || *s.Tools == "" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(*s.Tools), &names); err != nil {
		return nil
	}
	return names
}

// SetToolList encodes the skill's bound tool names.
func (s *ChatSkill) SetToolList(names []string) error {
	if len(names) == 0 {
		s.Tools = nil
		return nil
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return err
	}
	encoded := string(raw)
	s.Tools = &encoded
	return nil
}

func CreateChatSession(session *ChatSession) error {
	now := helper.GetTimestamp()
	session.CreatedTime = now
	session.UpdatedTime = now
	return DB.Create(session).Error
}

// GetChatSessionById loads a session owned by userId; ownership mismatch and
// missing rows are indistinguishable.
func GetChatSessionById(id int, userId int) (*ChatSession, error) {
	if id == 0 {
		return nil, errors.New("会话不存在")
	}
	var session ChatSession
	err := DB.First(&session, "id = ? and user_id = ?", id, userId).Error
	if err != nil {
		return nil, errors.New("会话不存在")
	}
	return &session, nil
}

func GetUserChatSessions(userId int) ([]*ChatSession, error) {
	var sessions []*ChatSession
	err := DB.Where("user_id = ?", userId).Order("updated_time desc").Find(&sessions).Error
	return sessions, err
}

func UpdateChatSession(session *ChatSession) error {
	session.UpdatedTime = helper.GetTimestamp()
	return DB.Model(session).Select("title", "model", "system_prompt", "temperature", "top_p",
		"max_tokens", "sandbox_enabled", "context_budget", "skill_ids", "summary",
		"compacted_through", "updated_time").Updates(session).Error
}

func DeleteChatSession(id int, userId int) error {
	session, err := GetChatSessionById(id, userId)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", session.Id).Delete(&ChatMessage{}).Error; err != nil {
			return err
		}
		return tx.Delete(session).Error
	})
}

func InsertChatMessage(message *ChatMessage) error {
	message.CreatedTime = helper.GetTimestamp()
	return DB.Create(message).Error
}

// UpdateChatMessage rewrites the mutable fields of one message row.
func UpdateChatMessage(message *ChatMessage) error {
	return DB.Model(message).Select("content", "tool_calls", "tokens").Updates(message).Error
}

func GetSessionMessages(sessionId int) ([]*ChatMessage, error) {
	var messages []*ChatMessage
	err := DB.Where("session_id = ?", sessionId).Order("id asc").Find(&messages).Error
	return messages, err
}

func EnsureChatToken(userId int) (*Token, error) {
	var token Token
	err := DB.Where("user_id = ? and name = ?", userId, ChatTokenName).Order("id asc").First(&token).Error
	if err == nil {
		if token.Status != TokenStatusEnabled {
			// The built-in chat cannot run without its billing token; restore
			// it rather than failing every request until the user notices.
			token.Status = TokenStatusEnabled
			if err := DB.Model(&token).Update("status", TokenStatusEnabled).Error; err != nil {
				return nil, err
			}
		}
		return &token, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	token = Token{
		UserId:         userId,
		Key:            random.GenerateKey(),
		Status:         TokenStatusEnabled,
		Name:           ChatTokenName,
		CreatedTime:    helper.GetTimestamp(),
		AccessedTime:   helper.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	if err := DB.Create(&token).Error; err != nil {
		// A concurrent first use may have created the token already.
		var existing Token
		if lookupErr := DB.Where("user_id = ? and name = ?", userId, ChatTokenName).
			Order("id asc").First(&existing).Error; lookupErr == nil {
			return &existing, nil
		}
		return nil, err
	}
	return &token, nil
}

// chatFileRoot returns the directory holding attachment payloads.
func chatFileRoot() string {
	return filepath.Join("data", "chat-files")
}

func SaveChatFilePayload(userId int, kind string, rawName string, payload []byte) (*ChatFile, error) {
	file := &ChatFile{
		UserId: userId,
		Name:   filepath.Base(rawName),
		Kind:   kind,
		Size:   int64(len(payload)),
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(file).Error; err != nil {
			return err
		}
		dir := chatFileRoot()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		path := filepath.Join(dir, fmt.Sprintf("%d_%d", userId, file.Id))
		if err := os.WriteFile(path, payload, 0600); err != nil {
			return err
		}
		file.Path = path
		return tx.Model(file).Update("path", file.Path).Error
	})
	if err != nil {
		return nil, err
	}
	return file, nil
}

func ReadChatFilePayload(file *ChatFile) ([]byte, error) {
	return os.ReadFile(file.Path)
}

func GetUserChatFiles(userId int) ([]*ChatFile, error) {
	var files []*ChatFile
	err := DB.Where("user_id = ?", userId).Order("id desc").Find(&files).Error
	return files, err
}

func GetChatFileById(id int, userId int) (*ChatFile, error) {
	if id == 0 {
		return nil, errors.New("文件不存在")
	}
	var file ChatFile
	err := DB.First(&file, "id = ? and user_id = ?", id, userId).Error
	if err != nil {
		return nil, errors.New("文件不存在")
	}
	return &file, nil
}

func DeleteChatFile(id int, userId int) error {
	file, err := GetChatFileById(id, userId)
	if err != nil {
		return err
	}
	if err := DB.Delete(file).Error; err != nil {
		return err
	}
	_ = os.Remove(file.Path)
	return nil
}

func ListAvailableSkills(userId int) ([]*ChatSkill, error) {
	var skills []*ChatSkill
	err := DB.Where("owner_id = ? or owner_id = ?", userId, ChatSkillOwnerGlobal).
		Order("owner_id asc, id asc").Find(&skills).Error
	return skills, err
}

func ListEnabledSkills(userId int) ([]*ChatSkill, error) {
	var skills []*ChatSkill
	err := DB.Where("(owner_id = ? or owner_id = ?) and enabled = ?", userId, ChatSkillOwnerGlobal, true).
		Order("owner_id asc, id asc").Find(&skills).Error
	return skills, err
}

func GetSkillForOwner(id int, ownerId int) (*ChatSkill, error) {
	var skill ChatSkill
	err := DB.First(&skill, "id = ? and owner_id = ?", id, ownerId).Error
	if err != nil {
		return nil, errors.New("Skill 不存在")
	}
	return &skill, nil
}

func UpsertChatSkill(skill *ChatSkill) error {
	now := helper.GetTimestamp()
	if skill.Id == 0 {
		skill.CreatedTime = now
		skill.UpdatedTime = now
		return DB.Create(skill).Error
	}
	skill.UpdatedTime = now
	return DB.Model(skill).Select("name", "description", "instructions", "tools", "enabled", "updated_time").
		Updates(skill).Error
}

func DeleteChatSkill(id int, ownerId int) error {
	skill, err := GetSkillForOwner(id, ownerId)
	if err != nil {
		return err
	}
	return DB.Delete(skill).Error
}

func ListMCPServers(userId int) ([]*ChatMCPServer, error) {
	var servers []*ChatMCPServer
	err := DB.Where("owner_id = ? or owner_id = ?", userId, ChatSkillOwnerGlobal).
		Order("owner_id asc, id asc").Find(&servers).Error
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		unsealMCPServerCredentials(server)
	}
	return servers, nil
}

func ListEnabledMCPServers(userId int) ([]*ChatMCPServer, error) {
	var servers []*ChatMCPServer
	err := DB.Where("(owner_id = ? or owner_id = ?) and enabled = ?", userId, ChatSkillOwnerGlobal, true).
		Order("owner_id asc, id asc").Find(&servers).Error
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		unsealMCPServerCredentials(server)
	}
	return servers, nil
}

func GetMCPServerForOwner(id int, ownerId int) (*ChatMCPServer, error) {
	var server ChatMCPServer
	err := DB.First(&server, "id = ? and owner_id = ?", id, ownerId).Error
	if err != nil {
		return nil, errors.New("MCP 服务器不存在")
	}
	unsealMCPServerCredentials(&server)
	return &server, nil
}

// unsealMCPServerCredentials decrypts the stored Env/Headers fields so every
// reader — the admin UI and the MCP dialer alike — sees the plaintext the
// admin entered.
func unsealMCPServerCredentials(server *ChatMCPServer) {
	unsealMCPCredentialField(fmt.Sprintf("MCP server %d env", server.Id), server.Env)
	unsealMCPCredentialField(fmt.Sprintf("MCP server %d headers", server.Id), server.Headers)
}

func UpsertChatMCPServer(server *ChatMCPServer) error {
	// User-supplied MCP credentials are secrets like any other: they land in
	// the database only sealed. Sealing writes into a copy so the caller's
	// struct — echoed back to the admin UI — keeps the entered plaintext.
	stored := *server
	if server.Env != nil {
		env := *server.Env
		stored.Env = &env
	}
	if server.Headers != nil {
		headers := *server.Headers
		stored.Headers = &headers
	}
	if err := sealMCPCredentialField(stored.Env); err != nil {
		return err
	}
	if err := sealMCPCredentialField(stored.Headers); err != nil {
		return err
	}
	now := helper.GetTimestamp()
	if stored.Id == 0 {
		stored.CreatedTime = now
		stored.UpdatedTime = now
		if err := DB.Create(&stored).Error; err != nil {
			return err
		}
		server.Id, server.CreatedTime, server.UpdatedTime =
			stored.Id, stored.CreatedTime, stored.UpdatedTime
		return nil
	}
	stored.UpdatedTime = now
	return DB.Model(&stored).Select("name", "transport", "command", "args", "env", "url", "headers", "enabled", "updated_time").
		Updates(&stored).Error
}

func DeleteChatMCPServer(id int, ownerId int) error {
	server, err := GetMCPServerForOwner(id, ownerId)
	if err != nil {
		return err
	}
	return DB.Delete(server).Error
}
