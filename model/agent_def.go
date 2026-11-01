package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common/helper"
)

// Agent definitions (R4): a definition answers "who executes, under which
// rules"; its versions are the immutable configurations runs pin. Owner 0
// marks a built-in agent managed by admins and visible to everyone; personal
// agents belong to their creating user. The plaza shows built-in and one's
// own definitions only — there is no public marketplace.

const (
	AgentModeChat = "chat"
	AgentModeWork = "work"
)

// ErrAgentVersionUnavailable wraps every reason a pinned version cannot
// start a new run (missing, invisible, archived, or mode mismatch) so
// callers can decide between user-visible messages and 404-style hides.
var ErrAgentVersionUnavailable = errors.New("agent version unavailable")

type AgentDefinition struct {
	Id int `json:"id"`
	// OwnerId 0 = built-in (admin-managed); personal agents carry their
	// creator's user id. (owner_id, name) is unique so the plaza stays
	// unambiguous per owner.
	OwnerId        int    `json:"owner_id" gorm:"index;uniqueIndex:idx_agent_def_owner_name"`
	Name           string `json:"name" gorm:"type:varchar(255);uniqueIndex:idx_agent_def_owner_name"`
	Description    string `json:"description" gorm:"type:varchar(1024)"`
	Archived       bool   `json:"archived" gorm:"default:false"`
	CurrentVersion int    `json:"current_version"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64  `json:"updated_time" gorm:"bigint"`
}

// AgentVersion is append-only: no code path updates or deletes rows, so a
// run that pinned a version id keeps its exact configuration snapshot even
// across later edits of the definition.
type AgentVersion struct {
	Id           int    `json:"id"`
	DefinitionId int    `json:"definition_id" gorm:"index;uniqueIndex:idx_agent_version_def_no"`
	Version      int    `json:"version" gorm:"uniqueIndex:idx_agent_version_def_no"`
	Spec         string `json:"spec" gorm:"type:text"`
	CreatedTime  int64  `json:"created_time" gorm:"bigint"`
}

// AgentVersionSpec is the frozen configuration payload of one version.
// SkillIds reference the runner-visible skills allowed by the agent: actual
// permissions are the intersection of the current user's enabled skills and
// this list, re-resolved at every execution so revocation stays immediate.
// FileIds are the version's reference files (the restricted attachment
// context): personal agents reference their author's uploads, built-in
// agents reference admin-published material delivered as part of the frozen
// configuration.
type AgentVersionSpec struct {
	Instructions   string   `json:"instructions"`
	Model          string   `json:"model"`
	SkillIds       []int    `json:"skill_ids"`
	FileIds        []int    `json:"file_ids"`
	SandboxEnabled bool     `json:"sandbox_enabled"`
	Modes          []string `json:"modes"`
	OpeningHint    string   `json:"opening_hint"`
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	MaxTokens      int      `json:"max_tokens"`
}

// Reference file limits: attachments ride every model leg, so the count and
// payload stay bounded instead of letting one version carry a corpus.
const (
	AgentMaxReferenceFiles     = 8
	AgentReferenceFileMaxBytes = int64(4 << 20)
	AgentReferenceFileMaxTotal = int64(8 << 20)
)

// normalizeAgentSpec clamps a submitted spec into its persisted form.
func normalizeAgentSpec(spec *AgentVersionSpec) error {
	spec.Instructions = trimAgentText(spec.Instructions, 32768)
	spec.Model = trimAgentText(spec.Model, 255)
	spec.OpeningHint = trimAgentText(spec.OpeningHint, 2048)
	if spec.MaxTokens < 0 || spec.MaxTokens > 16384 {
		return errors.New("max_tokens 须在 0-16384 之间")
	}
	if spec.Temperature != nil && (*spec.Temperature < 0 || *spec.Temperature > 2) {
		return errors.New("temperature 须在 0-2 之间")
	}
	if spec.TopP != nil && (*spec.TopP < 0 || *spec.TopP > 1) {
		return errors.New("top_p 须在 0-1 之间")
	}
	modes := make([]string, 0, 2)
	for _, mode := range spec.Modes {
		if mode == AgentModeChat || mode == AgentModeWork {
			if !slices.Contains(modes, mode) {
				modes = append(modes, mode)
			}
		}
	}
	if len(modes) == 0 {
		return errors.New("智能体须声明支持的运行模式（chat/work）")
	}
	spec.Modes = modes
	if len(spec.SkillIds) > 64 {
		return errors.New("Skill 绑定过多")
	}
	// Deduplicate and sanity-check the reference file ids; ownership and
	// payload limits are validated against the author's files in
	// validateAgentFileIds.
	ids := make([]int, 0, len(spec.FileIds))
	for _, id := range spec.FileIds {
		if id <= 0 || slices.Contains(ids, id) {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) > AgentMaxReferenceFiles {
		return fmt.Errorf("参考文件最多 %d 个", AgentMaxReferenceFiles)
	}
	spec.FileIds = ids
	return nil
}

func trimAgentText(text string, limit int) string {
	if len(text) > limit {
		text = text[:limit]
	}
	return text
}

// validateAgentSkillIds rejects references the owner cannot see: a version
// snapshot stores rules, and binding a skill the author does not control
// would silently grant nothing at run time anyway.
func validateAgentSkillIds(userId int, skillIds []int) error {
	if len(skillIds) == 0 {
		return nil
	}
	enabled, err := ListEnabledSkills(userId)
	if err != nil {
		return err
	}
	visible := make(map[int]bool, len(enabled))
	for _, skill := range enabled {
		visible[skill.Id] = true
	}
	for _, id := range skillIds {
		if !visible[id] {
			return fmt.Errorf("Skill #%d 不存在或不可见", id)
		}
	}
	return nil
}

// validateAgentFileIds rejects reference files the author does not own or
// that exceed the payload caps: a version snapshot stores configuration, and
// a file reference the author cannot control would either fail at run time
// or silently grant nothing.
func validateAgentFileIds(userId int, fileIds []int) error {
	if len(fileIds) == 0 {
		return nil
	}
	files, err := GetUserChatFiles(userId)
	if err != nil {
		return err
	}
	owned := make(map[int]*ChatFile, len(files))
	var total int64
	for _, file := range files {
		owned[file.Id] = file
	}
	for _, id := range fileIds {
		file, ok := owned[id]
		if !ok {
			return fmt.Errorf("参考文件 #%d 不存在或不可见", id)
		}
		if file.Size > AgentReferenceFileMaxBytes {
			return fmt.Errorf("参考文件 %s 超过单文件上限", file.Name)
		}
		total += file.Size
	}
	if total > AgentReferenceFileMaxTotal {
		return errors.New("参考文件总大小超过上限")
	}
	return nil
}

// CreateAgentDefinition persists a definition together with its first
// immutable version. skillIds are validated against the author's visible
// skills; userId 0 (built-in authorship) validates against admin-visible
// skills, which include the global ones.
func CreateAgentDefinition(def *AgentDefinition, userId int, spec *AgentVersionSpec) error {
	if err := normalizeAgentSpec(spec); err != nil {
		return err
	}
	if err := validateAgentSkillIds(userId, spec.SkillIds); err != nil {
		return err
	}
	if err := validateAgentFileIds(userId, spec.FileIds); err != nil {
		return err
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	now := helper.GetTimestamp()
	def.OwnerId = userId
	def.CurrentVersion = 1
	def.CreatedTime, def.UpdatedTime = now, now
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(def).Error; err != nil {
			return err
		}
		version := &AgentVersion{DefinitionId: def.Id, Version: 1,
			Spec: string(encoded), CreatedTime: now}
		return tx.Create(version).Error
	})
}

// CreateAgentVersion appends the next immutable version of a definition.
// Archived definitions reject new versions: unarchive first to edit.
func CreateAgentVersion(defId int, spec *AgentVersionSpec) (*AgentVersion, error) {
	def, err := GetAgentDefinition(defId)
	if err != nil {
		return nil, err
	}
	if err := normalizeAgentSpec(spec); err != nil {
		return nil, err
	}
	if err := validateAgentSkillIds(def.OwnerId, spec.SkillIds); err != nil {
		return nil, err
	}
	if err := validateAgentFileIds(def.OwnerId, spec.FileIds); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	version := &AgentVersion{DefinitionId: def.Id, Version: def.CurrentVersion + 1,
		Spec: string(encoded), CreatedTime: helper.GetTimestamp()}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if def.Archived {
			return errors.New("智能体已归档，无法保存新版本")
		}
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		return tx.Model(&AgentDefinition{}).Where("id = ?", def.Id).Updates(map[string]any{
			"current_version": version.Version, "updated_time": helper.GetTimestamp(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return version, nil
}

// UpdateAgentDefinitionMeta rewrites the mutable display fields of a
// definition. Versions are untouched; existing runs keep theirs.
func UpdateAgentDefinitionMeta(def *AgentDefinition) error {
	fields := map[string]any{
		"name": def.Name, "description": def.Description,
		"updated_time": helper.GetTimestamp(),
	}
	return DB.Model(&AgentDefinition{}).Where("id = ?", def.Id).Updates(fields).Error
}

// SetAgentDefinitionArchived toggles the archive flag. Archived definitions
// block new runs but keep history, runs and assets readable.
func SetAgentDefinitionArchived(defId int, archived bool) error {
	return DB.Model(&AgentDefinition{}).Where("id = ?", defId).Updates(map[string]any{
		"archived": archived, "updated_time": helper.GetTimestamp(),
	}).Error
}

func GetAgentDefinition(id int) (*AgentDefinition, error) {
	if id == 0 {
		return nil, errors.New("无效的智能体 ID")
	}
	var def AgentDefinition
	if err := DB.First(&def, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &def, nil
}

// GetAgentVersionById loads one immutable version row.
func GetAgentVersionById(id int) (*AgentVersion, error) {
	if id == 0 {
		return nil, errors.New("无效的智能体版本 ID")
	}
	var version AgentVersion
	if err := DB.First(&version, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

// GetLatestAgentVersion returns a definition's newest version row.
func GetLatestAgentVersion(definitionId int) (*AgentVersion, error) {
	var version AgentVersion
	if err := DB.Where("definition_id = ?", definitionId).
		Order("version desc").First(&version).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

// ListPlazaAgents returns the caller's plaza: every built-in definition and
// the user's own, archived ones included (they carry the flag and stay
// readable; only new starts are blocked).
func ListPlazaAgents(userId int) (builtin []*AgentDefinition, mine []*AgentDefinition, err error) {
	if err = DB.Where("owner_id = ?", 0).Order("id asc").Find(&builtin).Error; err != nil {
		return nil, nil, err
	}
	if err = DB.Where("owner_id = ?", userId).Order("id desc").Find(&mine).Error; err != nil {
		return nil, nil, err
	}
	return builtin, mine, nil
}

// ResolveAgentVersionForRun is the server-side gate for every run that pins
// an agent version: it hides other users' private definitions, blocks
// archived ones from starting new runs, enforces the declared mode support,
// and parses the frozen spec. Forged client-side versions cannot pass.
func ResolveAgentVersionForRun(userId, versionId int, mode string) (*AgentDefinition, *AgentVersion, *AgentVersionSpec, error) {
	version, err := GetAgentVersionById(versionId)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: 智能体版本不存在", ErrAgentVersionUnavailable)
	}
	def, err := GetAgentDefinition(version.DefinitionId)
	if err != nil || (def.OwnerId != 0 && def.OwnerId != userId) {
		return nil, nil, nil, fmt.Errorf("%w: 智能体不存在", ErrAgentVersionUnavailable)
	}
	if def.Archived {
		return nil, nil, nil, fmt.Errorf("%w: 智能体已归档，无法开始新运行", ErrAgentVersionUnavailable)
	}
	spec, err := ParseAgentVersionSpec(version)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %s", ErrAgentVersionUnavailable, err.Error())
	}
	if !slices.Contains(spec.Modes, mode) {
		return nil, nil, nil, fmt.Errorf("%w: 该智能体不支持此运行模式", ErrAgentVersionUnavailable)
	}
	return def, version, spec, nil
}

// ParseAgentVersionSpec decodes the frozen spec JSON of a version row.
func ParseAgentVersionSpec(version *AgentVersion) (*AgentVersionSpec, error) {
	var spec AgentVersionSpec
	if err := json.Unmarshal([]byte(version.Spec), &spec); err != nil {
		return nil, fmt.Errorf("智能体版本配置损坏: %w", err)
	}
	return &spec, nil
}
