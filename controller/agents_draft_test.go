package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/model"
)

// setupAgentControllerDB gives the controller tests an in-memory database
// with the tables the duplicate narrowing touches.
func setupAgentControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChatSkill{}, &model.ChatFile{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
	})
	return db
}

// TestParseAgentDraft pins the defensive extraction of the draft JSON from
// model output, including the fenced code blocks models add despite
// instructions and the missing-field rejection.
func TestParseAgentDraft(t *testing.T) {
	draft, err := parseAgentDraft(`{"name":"周报助手","description":"整理周报","instructions":"每周五整理团队周报。","opening_hint":"发我本周的工作记录吧"}`)
	if err != nil {
		t.Fatalf("plain json rejected: %v", err)
	}
	if draft["name"] != "周报助手" || draft["instructions"] == "" {
		t.Fatalf("draft fields wrong: %v", draft)
	}
	fenced, err := parseAgentDraft("```json\n{\"name\":\"助手\",\"description\":\"d\",\"instructions\":\"做事\",\"opening_hint\":\"hi\"}\n```")
	if err != nil || fenced["name"] != "助手" {
		t.Fatalf("fenced json rejected: %v %v", fenced, err)
	}
	if _, err := parseAgentDraft("这不是 JSON"); err == nil {
		t.Fatal("non-JSON output must be rejected")
	}
	if _, err := parseAgentDraft(`{"name":"x","description":"d","opening_hint":"h"}`); err == nil {
		t.Fatal("missing instructions must be rejected")
	}
}

// TestSpecVisibleToUser pins the duplicate narrowing: references the new
// owner cannot see are dropped from the copied spec, owned ones survive.
func TestSpecVisibleToUser(t *testing.T) {
	setupAgentControllerDB(t)
	owned := &model.ChatSkill{OwnerId: 9, Name: "我的规范", Instructions: "x", Enabled: true}
	if err := model.DB.Create(owned).Error; err != nil {
		t.Fatal(err)
	}
	file := &model.ChatFile{UserId: 9, Name: "mine.txt", Kind: model.ChatFileKindText}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}
	spec := &model.AgentVersionSpec{
		Modes:    []string{model.AgentModeChat},
		SkillIds: []int{owned.Id, 999},
		FileIds:  []int{file.Id, 888},
	}
	got, err := specVisibleToUser(spec, 9)
	if err != nil {
		t.Fatalf("specVisibleToUser failed: %v", err)
	}
	if len(got.SkillIds) != 1 || got.SkillIds[0] != owned.Id {
		t.Fatalf("skills must keep only visible ids, got %v", got.SkillIds)
	}
	if len(got.FileIds) != 1 || got.FileIds[0] != file.Id {
		t.Fatalf("files must keep only owned ids, got %v", got.FileIds)
	}
}
