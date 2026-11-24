package agent

import (
	"context"
	"encoding/json"

	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/norma-e/memory"
	normatool "github.com/infinmalum/norma-e/tool"
)

type userMemoryTool struct {
	normatool.CoreTool
	userID int
}

func (tool userMemoryTool) Call(ctx context.Context, input json.RawMessage, tc *normatool.ToolContext) (normatool.Result, error) {
	var result normatool.Result
	err := model.WithEnabledMemory(ctx, tool.userID, func() error {
		var runErr error
		result, runErr = tool.CoreTool.Call(ctx, input, tc)
		return runErr
	})
	return result, err
}

func addMemoryTools(ctx context.Context, set *toolSet, userId int) {
	if !model.MemoryEnabled(ctx, userId) {
		return
	}
	for _, core := range memory.Tools(model.UserMemoryStore(userId)) {
		set.addSDK(userMemoryTool{CoreTool: core, userID: userId})
	}
}
