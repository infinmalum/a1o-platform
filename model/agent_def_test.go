package model

import (
	"errors"
	"testing"
)

func openAgentDefTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	db := openAgentRunTestDB(t)
	DB = db
	if err := db.AutoMigrate(&AgentDefinition{}, &AgentVersion{}, &ChatSkill{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DB = previous })
}

func TestCreateAgentDefinitionFreezesFirstVersion(t *testing.T) {
	openAgentDefTestDB(t)
	skill := &ChatSkill{OwnerId: 42, Name: "表格规范", Instructions: "用表格输出。", Enabled: true}
	if err := DB.Create(skill).Error; err != nil {
		t.Fatal(err)
	}
	def := &AgentDefinition{Name: "数据助手", Description: "整理数据"}
	spec := &AgentVersionSpec{
		Instructions: "先核对数据再下结论。", Model: "test-model",
		SkillIds: []int{skill.Id}, SandboxEnabled: true, Modes: []string{AgentModeWork},
		OpeningHint: "帮我整理这份表格",
	}
	if err := CreateAgentDefinition(def, 42, spec); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if def.Id == 0 || def.OwnerId != 42 || def.CurrentVersion != 1 {
		t.Fatalf("definition not persisted correctly: %+v", def)
	}
	version, err := GetLatestAgentVersion(def.Id)
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != 1 || version.DefinitionId != def.Id {
		t.Fatalf("first version wrong: %+v", version)
	}
	parsed, err := ParseAgentVersionSpec(version)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Instructions != spec.Instructions || parsed.Model != spec.Model ||
		parsed.SandboxEnabled != true || len(parsed.Modes) != 1 || parsed.Modes[0] != AgentModeWork ||
		parsed.OpeningHint != spec.OpeningHint || len(parsed.SkillIds) != 1 || parsed.SkillIds[0] != skill.Id {
		t.Fatalf("spec did not round-trip: %+v", parsed)
	}
}

func TestAgentVersionsAreAppendOnly(t *testing.T) {
	openAgentDefTestDB(t)
	def := &AgentDefinition{Name: "论文助手"}
	if err := CreateAgentDefinition(def, 42, &AgentVersionSpec{
		Instructions: "第一版", Modes: []string{AgentModeChat, AgentModeWork}}); err != nil {
		t.Fatal(err)
	}
	first, err := GetLatestAgentVersion(def.Id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateAgentVersion(def.Id, &AgentVersionSpec{
		Instructions: "第二版", Modes: []string{AgentModeChat}})
	if err != nil {
		t.Fatalf("create second version: %v", err)
	}
	if second.Version != 2 || second.Id == first.Id {
		t.Fatalf("second version wrong: %+v", second)
	}
	reloaded, err := GetAgentVersionById(first.Id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Spec != first.Spec {
		t.Fatalf("first version row must never change: %q vs %q", reloaded.Spec, first.Spec)
	}
	fresh, _ := GetAgentDefinition(def.Id)
	if fresh.CurrentVersion != 2 {
		t.Fatalf("current version = %d, want 2", fresh.CurrentVersion)
	}
}

func TestResolveAgentVersionForRunGates(t *testing.T) {
	openAgentDefTestDB(t)
	personal := &AgentDefinition{Name: "私有助手"}
	if err := CreateAgentDefinition(personal, 42, &AgentVersionSpec{
		Instructions: "私有", Modes: []string{AgentModeChat}}); err != nil {
		t.Fatal(err)
	}
	builtin := &AgentDefinition{Name: "内置翻译"}
	if err := CreateAgentDefinition(builtin, 0, &AgentVersionSpec{
		Instructions: "内置", Modes: []string{AgentModeChat, AgentModeWork}}); err != nil {
		t.Fatal(err)
	}
	personalVersion, err := GetLatestAgentVersion(personal.Id)
	if err != nil {
		t.Fatal(err)
	}
	builtinVersion, err := GetLatestAgentVersion(builtin.Id)
	if err != nil {
		t.Fatal(err)
	}

	// Happy path: the owner resolves their own version.
	if _, _, spec, err := ResolveAgentVersionForRun(42, personalVersion.Id, AgentModeChat); err != nil {
		t.Fatalf("owner resolution failed: %v", err)
	} else if spec.Instructions != "私有" {
		t.Fatalf("spec = %+v", spec)
	}
	// Built-ins resolve for everyone.
	if _, _, _, err := ResolveAgentVersionForRun(7, builtinVersion.Id, AgentModeWork); err != nil {
		t.Fatalf("builtin resolution failed: %v", err)
	}
	// Another user's private definition reads as unavailable.
	if _, _, _, err := ResolveAgentVersionForRun(7, personalVersion.Id, AgentModeChat); !errors.Is(err, ErrAgentVersionUnavailable) {
		t.Fatalf("foreign private version must be unavailable, got %v", err)
	}
	// Mode mismatch is refused: work-only definitions cannot run chat turns.
	if _, _, _, err := ResolveAgentVersionForRun(42, personalVersion.Id, AgentModeWork); !errors.Is(err, ErrAgentVersionUnavailable) {
		t.Fatalf("mode mismatch must be unavailable, got %v", err)
	}
	// Unknown ids are unavailable.
	if _, _, _, err := ResolveAgentVersionForRun(42, 99999, AgentModeChat); !errors.Is(err, ErrAgentVersionUnavailable) {
		t.Fatalf("missing version must be unavailable, got %v", err)
	}
	// Archived definitions block new runs.
	if err := SetAgentDefinitionArchived(personal.Id, true); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ResolveAgentVersionForRun(42, personalVersion.Id, AgentModeChat); !errors.Is(err, ErrAgentVersionUnavailable) {
		t.Fatalf("archived version must be unavailable, got %v", err)
	}
}

func TestListPlazaAgentsScopesVisibility(t *testing.T) {
	openAgentDefTestDB(t)
	if err := CreateAgentDefinition(&AgentDefinition{Name: "内置A"}, 0,
		&AgentVersionSpec{Modes: []string{AgentModeChat}}); err != nil {
		t.Fatal(err)
	}
	if err := CreateAgentDefinition(&AgentDefinition{Name: "我的"}, 42,
		&AgentVersionSpec{Modes: []string{AgentModeChat}}); err != nil {
		t.Fatal(err)
	}
	if err := CreateAgentDefinition(&AgentDefinition{Name: "别人的"}, 43,
		&AgentVersionSpec{Modes: []string{AgentModeChat}}); err != nil {
		t.Fatal(err)
	}
	builtin, mine, err := ListPlazaAgents(42)
	if err != nil {
		t.Fatal(err)
	}
	if len(builtin) != 1 || builtin[0].Name != "内置A" {
		t.Fatalf("builtin plaza wrong: %+v", builtin)
	}
	if len(mine) != 1 || mine[0].Name != "我的" {
		t.Fatalf("my plaza wrong: %+v", mine)
	}
}

func TestCreateAgentDefinitionRejectsBadSpecs(t *testing.T) {
	openAgentDefTestDB(t)
	if err := CreateAgentDefinition(&AgentDefinition{Name: "无模式"}, 42,
		&AgentVersionSpec{Instructions: "x"}); err == nil {
		t.Fatal("empty modes must be rejected")
	}
	// Skill references the author cannot see are rejected up front.
	if err := CreateAgentDefinition(&AgentDefinition{Name: "越权Skill"}, 42,
		&AgentVersionSpec{Modes: []string{AgentModeChat}, SkillIds: []int{999}}); err == nil {
		t.Fatal("invisible skill binding must be rejected")
	}
}
