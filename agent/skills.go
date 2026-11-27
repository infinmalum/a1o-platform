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

type positionedSkillGuidance struct {
	text  string
	after int // Item count after the tool-result batch, or -1 until the batch is complete.
}

func (t *toolSet) rememberSkillGuidance(messages []llm.Message) {
	for _, msg := range messages {
		if _, ok := llm.SkillInvocationName(msg); ok {
			text := msg.Text()
			seen := false
			for _, guidance := range t.skillGuidance {
				if guidance.text == text {
					seen = true
					break
				}
			}
			if !seen {
				t.skillGuidance = append(t.skillGuidance, positionedSkillGuidance{text: text, after: -1})
			}
		}
	}
}

// Place guidance after the entire tool-result batch so no provider sees an
// intervening message between a tool call and its results. Keep it in memory.
func (t *toolSet) placeSkillGuidance(after int) {
	for i := len(t.skillGuidance) - 1; i >= 0 && t.skillGuidance[i].after < 0; i-- {
		t.skillGuidance[i].after = after
	}
}

func (t *toolSet) withSkillGuidance(items []Item) []Item {
	if len(t.skillGuidance) == 0 {
		return items
	}
	context := make([]Item, 0, len(items)+len(t.skillGuidance))
	var previous int
	for _, guidance := range t.skillGuidance {
		context = append(context, items[previous:guidance.after]...)
		context = append(context, Item{Type: ItemMessage, Role: "user",
			Content: []ContentPart{{Type: PartInputText, Text: guidance.text}}})
		previous = guidance.after
	}
	return append(context, items[previous:]...)
}
