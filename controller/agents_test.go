package controller

import (
	"testing"

	"github.com/infinmalum/a1o-platform/model"
)

func TestFilterSkillsInAgentScope(t *testing.T) {
	spec := &model.AgentVersionSpec{SkillIds: []int{2, 3}}
	skills := []*model.ChatSkill{{Id: 1, Name: "未绑定"}, {Id: 2, Name: "绑定"}, {Id: 3, Name: "也绑定"}}

	scoped := filterSkillsInAgentScope(skills, spec)
	if len(scoped) != 2 || scoped[0].Id != 2 || scoped[1].Id != 3 {
		t.Fatalf("scope filter wrong: %+v", scoped)
	}

	// An agent that binds no skills grants none, even if the session
	// selected some.
	if got := filterSkillsInAgentScope(skills, &model.AgentVersionSpec{}); len(got) != 0 {
		t.Fatalf("empty agent scope must grant nothing: %+v", got)
	}

	// Sessions without an agent keep their selections untouched.
	if got := filterSkillsInAgentScope(skills, nil); len(got) != 3 {
		t.Fatalf("nil spec must not filter: %+v", got)
	}
}
