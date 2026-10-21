package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/one-gateway/chat"
	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/common/ctxkey"
	"github.com/infinmalum/one-gateway/model"
)

// maxChatTextFileBytes caps inline text attachments; anything larger would
// blow the context budget on every turn.
const maxChatTextFileBytes = 256 << 10

// chatRunningSessions guards one in-flight turn per session per process.
var chatRunningSessions sync.Map

// chatError responds with the dashboard's success/message envelope.
func chatError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
}

type chatSessionPayload struct {
	Title          string   `json:"title"`
	Model          string   `json:"model"`
	SystemPrompt   string   `json:"system_prompt"`
	Temperature    *float64 `json:"temperature"`
	TopP           *float64 `json:"top_p"`
	MaxTokens      int      `json:"max_tokens"`
	SandboxEnabled bool     `json:"sandbox_enabled"`
	ContextBudget  int      `json:"context_budget"`
	SkillIds       []int    `json:"skill_ids"`
}

// validateChatSession clamps session fields into safe ranges.
func validateChatSession(payload *chatSessionPayload) error {
	if strings.TrimSpace(payload.Model) == "" {
		return errors.New("模型不能为空")
	}
	if payload.MaxTokens < 0 || payload.MaxTokens > 16384 {
		// Assistant rows are TEXT columns; the cap keeps one leg inside it.
		return errors.New("max_tokens 须在 0-16384 之间")
	}
	if payload.Temperature != nil && (*payload.Temperature < 0 || *payload.Temperature > 2) {
		return errors.New("temperature 须在 0-2 之间")
	}
	if payload.TopP != nil && (*payload.TopP < 0 || *payload.TopP > 1) {
		return errors.New("top_p 须在 0-1 之间")
	}
	if payload.ContextBudget != 0 && (payload.ContextBudget < 1024 || payload.ContextBudget > 200000) {
		return errors.New("上下文预算须为 0 或 1024-200000")
	}
	if len(payload.Title) > 255 {
		payload.Title = payload.Title[:255]
	}
	return nil
}

func applyChatSessionPayload(session *model.ChatSession, payload *chatSessionPayload) error {
	if err := validateChatSession(payload); err != nil {
		return err
	}
	session.Title = payload.Title
	session.Model = payload.Model
	session.SystemPrompt = payload.SystemPrompt
	session.Temperature = payload.Temperature
	session.TopP = payload.TopP
	session.MaxTokens = payload.MaxTokens
	session.SandboxEnabled = payload.SandboxEnabled
	session.ContextBudget = payload.ContextBudget
	return session.SetSkillIdList(payload.SkillIds)
}

// GetChatSessions lists the caller's sessions, newest activity first.
func GetChatSessions(c *gin.Context) {
	sessions, err := model.GetUserChatSessions(c.GetInt(ctxkey.Id))
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": sessions})
}

// CreateChatSession starts a new conversation.
func CreateChatSession(c *gin.Context) {
	var payload chatSessionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	session := &model.ChatSession{UserId: c.GetInt(ctxkey.Id)}
	if err := applyChatSessionPayload(session, &payload); err != nil {
		chatError(c, err)
		return
	}
	if session.Title == "" {
		session.Title = "新会话"
	}
	if err := model.CreateChatSession(session); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": session})
}

// GetChatSession loads one owned session.
func GetChatSession(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的会话 ID"))
		return
	}
	session, err := model.GetChatSessionById(id, c.GetInt(ctxkey.Id))
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": session})
}

// UpdateChatSession rewrites the session configuration.
func UpdateChatSession(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的会话 ID"))
		return
	}
	session, err := model.GetChatSessionById(id, c.GetInt(ctxkey.Id))
	if err != nil {
		chatError(c, err)
		return
	}
	var payload chatSessionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if err := applyChatSessionPayload(session, &payload); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpdateChatSession(session); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": session})
}

// DeleteChatSession removes the session and its messages.
func DeleteChatSession(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的会话 ID"))
		return
	}
	if err := model.DeleteChatSession(id, c.GetInt(ctxkey.Id)); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetChatSessionMessages returns the stored history, oldest first.
func GetChatSessionMessages(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的会话 ID"))
		return
	}
	session, err := model.GetChatSessionById(id, c.GetInt(ctxkey.Id))
	if err != nil {
		chatError(c, err)
		return
	}
	messages, err := model.GetSessionMessages(session.Id)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": messages})
}

// RunChatTurn persists the user message, then streams the turn as SSE.
func RunChatTurn(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的会话 ID"))
		return
	}
	session, err := model.GetChatSessionById(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	if _, busy := chatRunningSessions.LoadOrStore(session.Id, true); busy {
		chatError(c, errors.New("该会话正在回复中"))
		return
	}
	defer chatRunningSessions.Delete(session.Id)

	var request struct {
		Content string `json:"content"`
		FileIds []int  `json:"file_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		chatError(c, err)
		return
	}
	if strings.TrimSpace(request.Content) == "" && len(request.FileIds) == 0 {
		chatError(c, errors.New("消息内容不能为空"))
		return
	}

	var parts []model.ChatMessagePart
	for _, fileID := range request.FileIds {
		file, err := model.GetChatFileById(fileID, userId)
		if err != nil {
			chatError(c, err)
			return
		}
		kind := "text"
		if file.Kind == model.ChatFileKindImage {
			kind = "image_url"
		}
		parts = append(parts, model.ChatMessagePart{Type: kind, FileID: file.Id})
	}
	userMessage := &model.ChatMessage{
		SessionId: session.Id,
		Role:      model.ChatRoleUser,
		Content:   request.Content,
	}
	if len(parts) > 0 {
		raw, _ := json.Marshal(parts)
		encoded := string(raw)
		userMessage.Parts = &encoded
	}
	if err := model.InsertChatMessage(userMessage); err != nil {
		chatError(c, err)
		return
	}
	if session.Title == "" || session.Title == "新会话" {
		session.Title = truncateChatTitle(request.Content)
		_ = model.UpdateChatSession(session)
	}

	skills, err := sessionSkills(session, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	token, err := model.EnsureChatToken(userId)
	if err != nil {
		chatError(c, fmt.Errorf("计费令牌不可用: %w", err))
		return
	}
	history, err := model.GetSessionMessages(session.Id)
	if err != nil {
		chatError(c, err)
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		chatError(c, errors.New("当前连接不支持流式响应"))
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	emit := func(event chat.Event) error {
		if _, err := c.Writer.Write(chat.EncodeEvent(event)); err != nil {
			cancel()
			return err
		}
		flusher.Flush()
		return nil
	}
	turn := chat.TurnRequest{
		Session:       session,
		History:       history,
		Skills:        skills,
		Leg:           chat.NewLifecycleLeg(session.Model, userId, token.Id, token.Name),
		ContextBudget: session.ContextBudget,
	}
	_ = chat.Default().Run(ctx, emit, turn)
}

// truncateChatTitle derives a session title from the first user message.
func truncateChatTitle(content string) string {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	runes := []rune(content)
	if len(runes) > 50 {
		return string(runes[:50])
	}
	if content == "" {
		return "新会话"
	}
	return content
}

// sessionSkills resolves the session's selected skill IDs against the
// enabled skills visible to the user.
func sessionSkills(session *model.ChatSession, userId int) ([]*model.ChatSkill, error) {
	ids := session.SkillIdList()
	if len(ids) == 0 {
		return nil, nil
	}
	skills, err := model.ListEnabledSkills(userId)
	if err != nil {
		return nil, err
	}
	selected := map[int]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	var matched []*model.ChatSkill
	for _, skill := range skills {
		if selected[skill.Id] {
			matched = append(matched, skill)
		}
	}
	return matched, nil
}

// GetChatFiles lists the caller's uploaded files.
func GetChatFiles(c *gin.Context) {
	files, err := model.GetUserChatFiles(c.GetInt(ctxkey.Id))
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": files})
}

// UploadChatFile accepts one multipart attachment under "file".
func UploadChatFile(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, config.ChatMaxFileBytes+(64<<10))
	fileHeader, err := c.FormFile("file")
	if err != nil {
		chatError(c, fmt.Errorf("读取上传失败: %w", err))
		return
	}
	if fileHeader.Size > config.ChatMaxFileBytes {
		chatError(c, fmt.Errorf("文件超过大小上限 %d 字节", config.ChatMaxFileBytes))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		chatError(c, err)
		return
	}
	defer file.Close()
	payload, err := io.ReadAll(file)
	if err != nil {
		chatError(c, err)
		return
	}
	kind := model.ChatFileKindText
	contentType := http.DetectContentType(payload)
	if strings.HasPrefix(contentType, "image/") {
		kind = model.ChatFileKindImage
	} else if int64(len(payload)) > maxChatTextFileBytes {
		chatError(c, fmt.Errorf("文本文件须在 %d 字节以内", maxChatTextFileBytes))
		return
	}
	stored, err := model.SaveChatFilePayload(userId, kind, fileHeader.Filename, payload)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stored})
}

// DeleteChatFile removes one owned file.
func DeleteChatFile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的文件 ID"))
		return
	}
	if err := model.DeleteChatFile(id, c.GetInt(ctxkey.Id)); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetChatBootstrap exposes runtime limits for the chat UI.
func GetChatBootstrap(c *gin.Context) {
	runtime := chat.Default()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"sandbox_available":   runtime.Sandbox.Available(),
		"sandbox_enabled":     config.ChatSandboxEnabled,
		"budget":              config.ChatContextBudgetTokens,
		"max_tool_iterations": config.ChatMaxToolIterations,
		"max_file_bytes":      config.ChatMaxFileBytes,
		"max_text_file_bytes": maxChatTextFileBytes,
	}})
}

// ---- Skills ----

type chatSkillPayload struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
	Enabled      bool     `json:"enabled"`
}

func applyChatSkillPayload(skill *model.ChatSkill, payload *chatSkillPayload) error {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return errors.New("Skill 名称不能为空")
	}
	if len(name) > 255 {
		name = name[:255]
	}
	skill.Name = name
	skill.Description = payload.Description
	skill.Instructions = payload.Instructions
	skill.Enabled = payload.Enabled
	return skill.SetToolList(payload.Tools)
}

// chatSkillViews projects skills for the API with their stored JSON columns
// decoded; the result is never nil so empty lists serialize as [].
func chatSkillViews(skills []*model.ChatSkill) []model.ChatSkillView {
	views := make([]model.ChatSkillView, 0, len(skills))
	for _, skill := range skills {
		views = append(views, skill.View())
	}
	return views
}

// chatMCPServerViews projects MCP servers for the API with their stored JSON
// columns decoded; the result is never nil so empty lists serialize as [].
func chatMCPServerViews(servers []*model.ChatMCPServer) []model.ChatMCPServerView {
	views := make([]model.ChatMCPServerView, 0, len(servers))
	for _, server := range servers {
		views = append(views, server.View())
	}
	return views
}

// GetChatSkills lists global and personal skills for the caller.
func GetChatSkills(c *gin.Context) {
	skills, err := model.ListAvailableSkills(c.GetInt(ctxkey.Id))
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": chatSkillViews(skills)})
}

// CreateChatSkill creates a personal skill.
func CreateChatSkill(c *gin.Context) {
	var payload chatSkillPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	skill := &model.ChatSkill{OwnerId: c.GetInt(ctxkey.Id), Enabled: true}
	if err := applyChatSkillPayload(skill, &payload); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatSkill(skill); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": skill.View()})
}

// UpdateChatSkill rewrites one personal skill.
func UpdateChatSkill(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 Skill ID"))
		return
	}
	skill, err := model.GetSkillForOwner(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	var payload chatSkillPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if err := applyChatSkillPayload(skill, &payload); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatSkill(skill); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": skill.View()})
}

// DeleteChatSkill removes one personal skill.
func DeleteChatSkill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 Skill ID"))
		return
	}
	if err := model.DeleteChatSkill(id, c.GetInt(ctxkey.Id)); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetGlobalChatSkills lists administrator-managed skills.
func GetGlobalChatSkills(c *gin.Context) {
	skills, err := model.ListAvailableSkills(model.ChatSkillOwnerGlobal)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": chatSkillViews(skills)})
}

// CreateGlobalChatSkill creates a global skill.
func CreateGlobalChatSkill(c *gin.Context) {
	var payload chatSkillPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	skill := &model.ChatSkill{OwnerId: model.ChatSkillOwnerGlobal, Enabled: true}
	if err := applyChatSkillPayload(skill, &payload); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatSkill(skill); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": skill.View()})
}

// UpdateGlobalChatSkill rewrites one global skill.
func UpdateGlobalChatSkill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 Skill ID"))
		return
	}
	skill, err := model.GetSkillForOwner(id, model.ChatSkillOwnerGlobal)
	if err != nil {
		chatError(c, err)
		return
	}
	var payload chatSkillPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if err := applyChatSkillPayload(skill, &payload); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatSkill(skill); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": skill.View()})
}

// DeleteGlobalChatSkill removes one global skill.
func DeleteGlobalChatSkill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 Skill ID"))
		return
	}
	if err := model.DeleteChatSkill(id, model.ChatSkillOwnerGlobal); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ---- MCP servers ----

type chatMCPPayload struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Enabled   bool              `json:"enabled"`
}

// encodeChatJSON marshals optional JSON columns; nil maps to a nil pointer.
func encodeChatJSON(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded := string(raw)
	return &encoded, nil
}

func applyChatMCPPayload(server *model.ChatMCPServer, payload *chatMCPPayload, allowStdio bool) error {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return errors.New("MCP 服务器名称不能为空")
	}
	server.Name = name
	server.Transport = payload.Transport
	server.Enabled = payload.Enabled
	switch payload.Transport {
	case model.ChatMCPServerHTTP:
		if strings.TrimSpace(payload.URL) == "" {
			return errors.New("HTTP MCP 服务器必须填写 URL")
		}
		server.URL = strings.TrimSpace(payload.URL)
		server.Command = ""
		server.Args, server.Env = nil, nil
	case model.ChatMCPServerStdio:
		if !allowStdio {
			return errors.New("个人配置仅支持 HTTP 传输")
		}
		if strings.TrimSpace(payload.Command) == "" {
			return errors.New("stdio MCP 服务器必须填写命令")
		}
		server.Command = strings.TrimSpace(payload.Command)
		server.URL = ""
		args, err := encodeChatJSON(payload.Args)
		if err != nil {
			return err
		}
		env, err := encodeChatJSON(payload.Env)
		if err != nil {
			return err
		}
		server.Args, server.Env = args, env
	default:
		return fmt.Errorf("未知传输类型 %q", payload.Transport)
	}
	headers, err := encodeChatJSON(payload.Headers)
	if err != nil {
		return err
	}
	server.Headers = headers
	return nil
}

// GetChatMCPServers lists the global MCP servers (administrator view).
func GetChatMCPServers(c *gin.Context) {
	servers, err := model.ListMCPServers(model.ChatSkillOwnerGlobal)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": chatMCPServerViews(servers)})
}

// CreateChatMCPServer creates a global MCP server.
func CreateChatMCPServer(c *gin.Context) {
	var payload chatMCPPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	server := &model.ChatMCPServer{OwnerId: model.ChatSkillOwnerGlobal, Enabled: true}
	if err := applyChatMCPPayload(server, &payload, true); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatMCPServer(server); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": server.View()})
}

// UpdateChatMCPServer rewrites one global MCP server.
func UpdateChatMCPServer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 MCP 服务器 ID"))
		return
	}
	server, err := model.GetMCPServerForOwner(id, model.ChatSkillOwnerGlobal)
	if err != nil {
		chatError(c, err)
		return
	}
	var payload chatMCPPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if err := applyChatMCPPayload(server, &payload, true); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatMCPServer(server); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": server.View()})
}

// DeleteChatMCPServer removes one global MCP server.
func DeleteChatMCPServer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 MCP 服务器 ID"))
		return
	}
	if err := model.DeleteChatMCPServer(id, model.ChatSkillOwnerGlobal); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetChatUserMCPServers lists the caller's own HTTP MCP servers.
func GetChatUserMCPServers(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	servers, err := model.ListMCPServers(userId)
	if err != nil {
		chatError(c, err)
		return
	}
	var owned []*model.ChatMCPServer
	for _, server := range servers {
		if server.OwnerId == userId {
			owned = append(owned, server)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": chatMCPServerViews(owned)})
}

// CreateChatUserMCPServer creates a personal HTTP MCP server.
func CreateChatUserMCPServer(c *gin.Context) {
	var payload chatMCPPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	server := &model.ChatMCPServer{OwnerId: c.GetInt(ctxkey.Id), Enabled: true}
	if err := applyChatMCPPayload(server, &payload, false); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatMCPServer(server); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": server.View()})
}

// UpdateChatUserMCPServer rewrites one personal HTTP MCP server.
func UpdateChatUserMCPServer(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 MCP 服务器 ID"))
		return
	}
	server, err := model.GetMCPServerForOwner(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	var payload chatMCPPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if err := applyChatMCPPayload(server, &payload, false); err != nil {
		chatError(c, err)
		return
	}
	if err := model.UpsertChatMCPServer(server); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": server.View()})
}

// DeleteChatUserMCPServer removes one personal MCP server.
func DeleteChatUserMCPServer(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的 MCP 服务器 ID"))
		return
	}
	if err := model.DeleteChatMCPServer(id, userId); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
