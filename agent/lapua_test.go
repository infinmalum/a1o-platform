package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestLapuaStopsDispatchAfterToolResultPersistenceFails(t *testing.T) {
	tools := newToolSet()
	var executions atomic.Int32
	tools.add(Tool{Name: "test_tool"}, func(context.Context, toolRequest) toolResult {
		executions.Add(1)
		return toolResult{Content: "ok"}
	})

	saveErr := errors.New("tool result could not be saved")
	legs := 0
	err := lapuaLoop(t.Context(), tools, 2, func(context.Context) (InvokeResult, []Item, error) {
		legs++
		return InvokeResult{}, []Item{
			{CallID: "first", Name: "test_tool", Arguments: "{}"},
			{CallID: "second", Name: "test_tool", Arguments: "{}"},
		}, nil
	}, func(_ InvokeResult, call Item, result toolResult) error {
		if call.CallID != "first" || result.IsError || result.Content != "ok" {
			t.Fatalf("unexpected result for %q: %+v", call.CallID, result)
		}
		return saveErr
	})

	if !errors.Is(err, saveErr) || legs != 1 || executions.Load() != 1 {
		t.Fatalf("got err=%v, model legs=%d, tool executions=%d", err, legs, executions.Load())
	}
}
