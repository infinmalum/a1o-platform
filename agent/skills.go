package agent

import (
	"fmt"
	"strings"

	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/norma-e/llm"
	sdk "github.com/infinmalum/norma-e/skill"
)

const skillToolName = "Skill"

func (t *toolSet) addSkillTool(skills []*model.ChatSkill) {
	registry := sdk.NewRegistry()
	for _, skill := range skills {
		if strings.TrimSpace(skill.Instructions) == "" {
			continue
		}
		description := skill.Name
		if summary := strings.TrimSpace(skill.Description); summary != "" {
			description += " — " + summary
		}
		registry.Add(sdk.Skill{
			Name:         fmt.Sprintf("skill-%d", skill.Id),
			Description:  strings.Join(strings.Fields(description), " "),
			Instructions: skill.Instructions,
		})
	}
	if len(registry.List()) == 0 {
		return
	}
	core := registry.Tool()
	t.addSDK(core)
	t.skillPrompt = core.Prompt() + "\nCall Skill before using a listed procedure if its instructions are absent from the current context."
}

// Only the current execution carries loaded instructions. The saved tool
// result keeps its acknowledgement, so a new execution resolves skills anew.
func (t *toolSet) rememberSkillGuidance(messages []llm.Message) {
	for _, msg := range messages {
		name, ok := llm.SkillInvocationName(msg)
		if !ok {
			continue
		}
		if t.skillGuidance == nil {
			t.skillGuidance = make(map[string]string)
		}
		if _, loaded := t.skillGuidance[name]; !loaded {
			t.loadedSkills = append(t.loadedSkills, name)
		}
		t.skillGuidance[name] = msg.Text()
	}
}

func (t *toolSet) withSkillGuidance(items []Item) []Item {
	if len(t.loadedSkills) == 0 {
		return items
	}
	context := append([]Item(nil), items...)
	for _, name := range t.loadedSkills {
		context = append(context, Item{Type: ItemMessage, Role: "user",
			Content: []ContentPart{{Type: PartInputText, Text: t.skillGuidance[name]}}})
	}
	return context
}
