package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

// Agent plaza API (R4): built-in agents (admin-managed, owner 0) and the
// caller's own agents. There is no public marketplace — the plaza only ever
// shows those two sections, and edits always append a new immutable version
// instead of rewriting history.

type agentPayload struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Instructions   string   `json:"instructions"`
	Model          string   `json:"model"`
	SkillIds       []int    `json:"skill_ids"`
	FileIds        []int    `json:"file_ids"`
	SandboxEnabled bool     `json:"sandbox_enabled"`
	Modes          []string `json:"modes"`
	OpeningHint    string   `json:"opening_hint"`
	Temperature    *float64 `json:"temperature"`
	TopP           *float64 `json:"top_p"`
	MaxTokens      int      `json:"max_tokens"`
}

// agentVersionInfo exposes the pinned version's identity without repeating
// the raw spec JSON the Spec field already carries parsed.
type agentVersionInfo struct {
	Id          int   `json:"id"`
	Version     int   `json:"version"`
	CreatedTime int64 `json:"created_time"`
}

type agentView struct {
	model.AgentDefinition
	Latest *agentVersionInfo       `json:"latest"`
	Spec   *model.AgentVersionSpec `json:"spec"`
}

func buildAgentView(def *model.AgentDefinition) (*agentView, error) {
	version, err := model.GetLatestAgentVersion(def.Id)
	if err != nil {
		return nil, err
	}
	spec, err := model.ParseAgentVersionSpec(version)
	if err != nil {
		return nil, err
	}
	return &agentView{
		AgentDefinition: *def,
		Latest:          &agentVersionInfo{Id: version.Id, Version: version.Version, CreatedTime: version.CreatedTime},
		Spec:            spec,
	}, nil
}

func specFromAgentPayload(payload *agentPayload) *model.AgentVersionSpec {
	return &model.AgentVersionSpec{
		Instructions:   payload.Instructions,
		Model:          strings.TrimSpace(payload.Model),
		SkillIds:       payload.SkillIds,
		FileIds:        payload.FileIds,
		SandboxEnabled: payload.SandboxEnabled,
		Modes:          payload.Modes,
		OpeningHint:    payload.OpeningHint,
		Temperature:    payload.Temperature,
		TopP:           payload.TopP,
		MaxTokens:      payload.MaxTokens,
	}
}

// agentDefAuthorizes decides who may mutate a definition: built-ins only
// accept admins, personal definitions only their owner.
func agentDefAuthorizes(def *model.AgentDefinition, userId int, isAdmin bool) bool {
	if def.OwnerId == 0 {
		return isAdmin
	}
	return def.OwnerId == userId
}

// isAdminContext reports whether the authenticated session carries at least
// the admin role.
func isAdminContext(c *gin.Context) bool {
	return c.GetInt("role") >= model.RoleAdminUser
}

func agentNameConflict(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return errors.New("同名智能体已存在")
	}
	return err
}

// GetAgentPlaza lists the caller's plaza: built-in and own definitions,
// archived ones included and flagged (history stays readable; only new
// starts are blocked server-side).
func GetAgentPlaza(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	builtin, mine, err := model.ListPlazaAgents(userId)
	if err != nil {
		chatError(c, err)
		return
	}
	views := func(defs []*model.AgentDefinition) []*agentView {
		result := make([]*agentView, 0, len(defs))
		for _, def := range defs {
			view, err := buildAgentView(def)
			if err != nil {
				continue
			}
			result = append(result, view)
		}
		return result
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"builtin": views(builtin), "mine": views(mine),
	}})
}

// GetAgentDetail returns one visible definition with its latest frozen spec.
// Other users' private definitions read as nonexistent.
func GetAgentDetail(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	def, err := loadVisibleAgent(c, userId)
	if err != nil {
		return
	}
	view, err := buildAgentView(def)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// loadVisibleAgent fetches the definition and enforces plaza visibility:
// built-ins are public, personal ones only for their owner.
func loadVisibleAgent(c *gin.Context, userId int) (*model.AgentDefinition, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的智能体 ID"))
		return nil, err
	}
	def, err := model.GetAgentDefinition(id)
	if err != nil || (def.OwnerId != 0 && def.OwnerId != userId) {
		chatError(c, errors.New("智能体不存在"))
		return nil, err
	}
	return def, nil
}

func bindAgentPayload(c *gin.Context) (*agentPayload, error) {
	var payload agentPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return nil, err
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		chatError(c, errors.New("智能体名称不能为空"))
		return nil, errors.New("empty name")
	}
	if len(payload.Name) > 255 {
		payload.Name = payload.Name[:255]
	}
	return &payload, nil
}

// CreateAgent creates a personal definition with its first version.
func CreateAgent(c *gin.Context) {
	payload, err := bindAgentPayload(c)
	if err != nil {
		return
	}
	def := &model.AgentDefinition{Name: payload.Name, Description: payload.Description}
	if err := model.CreateAgentDefinition(def, c.GetInt(ctxkey.Id), specFromAgentPayload(payload)); err != nil {
		chatError(c, agentNameConflict(err))
		return
	}
	view, err := buildAgentView(def)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// CreateBuiltinAgent creates an admin-owned built-in definition.
func CreateBuiltinAgent(c *gin.Context) {
	payload, err := bindAgentPayload(c)
	if err != nil {
		return
	}
	def := &model.AgentDefinition{Name: payload.Name, Description: payload.Description}
	if err := model.CreateAgentDefinition(def, 0, specFromAgentPayload(payload)); err != nil {
		chatError(c, agentNameConflict(err))
		return
	}
	view, err := buildAgentView(def)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// UpdateAgent rewrites the mutable display fields and appends a new
// immutable version. Built-ins accept admins only, personal ones only their
// owner; neither path can touch another user's definitions.
func UpdateAgent(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	def, err := loadVisibleAgent(c, userId)
	if err != nil {
		return
	}
	if !agentDefAuthorizes(def, userId, isAdminContext(c)) {
		chatError(c, errors.New("无权修改该智能体"))
		return
	}
	payload, err := bindAgentPayload(c)
	if err != nil {
		return
	}
	if def.Archived {
		chatError(c, errors.New("智能体已归档，请先取消归档"))
		return
	}
	def.Name = payload.Name
	def.Description = payload.Description
	if err := model.UpdateAgentDefinitionMeta(def); err != nil {
		chatError(c, agentNameConflict(err))
		return
	}
	if _, err := model.CreateAgentVersion(def.Id, specFromAgentPayload(payload)); err != nil {
		chatError(c, agentNameConflict(err))
		return
	}
	// Reload so the view carries the bumped current_version.
	fresh, err := model.GetAgentDefinition(def.Id)
	if err != nil {
		chatError(c, err)
		return
	}
	view, err := buildAgentView(fresh)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// ArchiveAgent toggles the archive flag: archived definitions block new
// runs but keep sessions, runs and assets readable.
func ArchiveAgent(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	def, err := loadVisibleAgent(c, userId)
	if err != nil {
		return
	}
	if !agentDefAuthorizes(def, userId, isAdminContext(c)) {
		chatError(c, errors.New("无权修改该智能体"))
		return
	}
	var payload struct {
		Archived bool `json:"archived"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if err := model.SetAgentDefinitionArchived(def.Id, payload.Archived); err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"archived": payload.Archived}})
}

// DuplicateAgent copies a visible definition into a fresh personal one: the
// plaza's built-ins are reusable by every user, own definitions by their
// owner. The latest frozen spec carries over with one adjustment — skill and
// file references the duplicating user cannot see are dropped, because a
// personal copy runs under its owner's permissions and neither grants nor
// carries another author's material. The copy starts unarchived at version 1.
func DuplicateAgent(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	source, err := loadVisibleAgent(c, userId)
	if err != nil {
		return
	}
	view, err := buildAgentView(source)
	if err != nil {
		chatError(c, err)
		return
	}
	name := strings.TrimSpace(source.Name + " 副本")
	if len(name) > 255 {
		name = name[:250] + " 副本"
	}
	spec, err := specVisibleToUser(view.Spec, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	duplicated := &model.AgentDefinition{Name: name, Description: source.Description}
	if err := model.CreateAgentDefinition(duplicated, userId, spec); err != nil {
		chatError(c, agentNameConflict(err))
		return
	}
	result, err := buildAgentView(duplicated)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// specVisibleToUser narrows a copied spec's skill and file references to
// what the new owner can actually see, mirroring the run-time intersection
// semantics instead of carrying invisible references into a new version.
func specVisibleToUser(spec *model.AgentVersionSpec, userId int) (*model.AgentVersionSpec, error) {
	if spec == nil {
		return nil, errors.New("智能体缺少可用版本")
	}
	if len(spec.SkillIds) > 0 {
		enabled, err := model.ListEnabledSkills(userId)
		if err != nil {
			return nil, err
		}
		visible := make(map[int]bool, len(enabled))
		for _, skill := range enabled {
			visible[skill.Id] = true
		}
		kept := spec.SkillIds[:0]
		for _, id := range spec.SkillIds {
			if visible[id] {
				kept = append(kept, id)
			}
		}
		spec.SkillIds = kept
	}
	if len(spec.FileIds) > 0 {
		files, err := model.GetUserChatFiles(userId)
		if err != nil {
			return nil, err
		}
		owned := make(map[int]bool, len(files))
		for _, file := range files {
			owned[file.Id] = true
		}
		kept := spec.FileIds[:0]
		for _, id := range spec.FileIds {
			if owned[id] {
				kept = append(kept, id)
			}
		}
		spec.FileIds = kept
	}
	return spec, nil
}

// DraftAgent turns a natural-language description into a draft configuration
// through one billed LLM call. The output is a draft only: the client shows
// it in the editor for review before anything is saved.
func DraftAgent(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var payload struct {
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	description := strings.TrimSpace(payload.Description)
	if description == "" {
		chatError(c, errors.New("请描述你想创建的智能体"))
		return
	}
	if len(description) > 4000 {
		description = description[:4000]
	}
	system := "你是智能体配置助手。根据用户的描述，生成一个 AI 智能体的草稿配置。" +
		"只输出一个 JSON 对象，不要输出任何其他文字或代码块标记，字段如下：" +
		`{"name": "简短名称（不超过20字）", "description": "一句话用途说明", ` +
		`"instructions": "给模型的系统指令，写明角色、任务规则、输出要求，使用第二人称", ` +
		`"opening_hint": "给用户的开场建议语"}` +
		" 所有字段都必须是中文字符串。"
	result, err := callLLMOnce(c.Request.Context(), userId, strings.TrimSpace(payload.Model),
		system, description, 2048, nil)
	if err != nil {
		chatError(c, fmt.Errorf("草稿生成失败: %w", err))
		return
	}
	draft, err := parseAgentDraft(result.Content)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": draft})
}

// parseAgentDraft extracts the draft JSON from the model output, tolerating
// the fenced code blocks models add despite instructions.
func parseAgentDraft(content string) (gin.H, error) {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return nil, errors.New("模型未返回有效的草稿 JSON")
	}
	var draft map[string]string
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &draft); err != nil {
		return nil, errors.New("模型返回的草稿格式无效")
	}
	result := gin.H{}
	for _, field := range []string{"name", "description", "instructions", "opening_hint"} {
		result[field] = strings.TrimSpace(draft[field])
	}
	if result["name"] == "" || result["instructions"] == "" {
		return nil, errors.New("模型草稿缺少名称或指令")
	}
	return result, nil
}
