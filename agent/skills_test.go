package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infinmalum/a1o-platform/model"
)

func TestSkillLoadsOnDemandInChatAndWork(t *testing.T) {
	const instructions = "End the report with a verification checklist."
	skill := &model.ChatSkill{Id: 7, Name: "报告校验", Description: "Verify completed reports.", Instructions: instructions}
	require.NoError(t, skill.SetToolList([]string{"echo"}))

	for _, mode := range []string{"chat", "work"} {
		t.Run(mode, func(t *testing.T) {
			executor := &fakeExecutor{responses: []InvokeResult{
				{Items: []Item{
					{Type: ItemFunctionCall, CallID: "bad", Name: skillToolName, Arguments: `{"name":"skill-8"}`},
					{Type: ItemFunctionCall, CallID: "load", Name: skillToolName, Arguments: `{"name":"skill-7"}`},
				}},
				assistantLeg("Verified."),
			}}
			engine := &Engine{}
			emit := func(EventEnvelope) error { return nil }
			if mode == "chat" {
				setupChatDB(t)
				session, run, history := newTurnFixture(t, "Keep replies brief.")
				require.NoError(t, engine.RunChat(context.Background(), emit, ChatTurnRequest{
					Session: session, Run: run, History: history, Executor: executor, Skills: []*model.ChatSkill{skill},
				}))
				rows, err := model.GetSessionMessages(session.Id)
				require.NoError(t, err)
				stored, err := json.Marshal(rows)
				require.NoError(t, err)
				require.NotContains(t, string(stored), instructions)
			} else {
				openWorkTestDB(t)
				task, run := newWorkFixture(t, 1)
				fencing := claimFixture(t, run)
				executor.responses[1].Items = append(executor.responses[1].Items, Item{
					Type: ItemFunctionCall, CallID: "done", Name: CompleteTaskToolName, Arguments: `{"summary":"Verified."}`,
				})
				require.NoError(t, engine.RunWork(context.Background(), WorkTurnRequest{
					Run: run, Fencing: fencing, Task: task, Executor: executor, Skills: []*model.ChatSkill{skill}, Emit: emit,
				}))
				stored, err := model.GetAgentRunById(run.Id)
				require.NoError(t, err)
				require.Equal(t, model.RunStatusCompleted, stored.Status)
				require.NotContains(t, *stored.ContextItems, instructions)
			}
			require.Len(t, executor.requests, 2)
			first, next := executor.requests[0], executor.requests[1]
			require.Contains(t, first.Instructions, "skill-7: 报告校验")
			require.NotContains(t, first.Instructions, instructions)
			firstItems, err := json.Marshal(first.Items)
			require.NoError(t, err)
			require.NotContains(t, string(firstItems), instructions)
			require.NotEmpty(t, next.Items)
			guidance := next.Items[len(next.Items)-1]
			require.Equal(t, "user", guidance.Role)
			require.Len(t, guidance.Content, 1)
			require.Contains(t, guidance.Content[0].Text, instructions)
			require.NotContains(t, next.Instructions, instructions)
			for _, tool := range first.Tools {
				require.Contains(t, []string{skillToolName, CompleteTaskToolName}, tool.Name)
			}
			require.NotEmpty(t, first.Tools)
		})
	}
}
