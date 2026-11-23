package agent

import (
	"context"
	"encoding/json"

	"github.com/infinmalum/Lapua/memory"
	lapuatool "github.com/infinmalum/Lapua/tool"
	"github.com/infinmalum/a1o-platform/model"
)

type userMemoryTool struct {
	lapuatool.CoreTool
	userID int
}

func (tool userMemoryTool) Call(ctx context.Context, input json.RawMessage, tc *lapuatool.ToolContext) (lapuatool.Result, error) {
	var result lapuatool.Result
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
