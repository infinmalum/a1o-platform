package agent

import (
	"testing"
)

// feedLines pushes SSE data lines through the collector's Write.
func feedLines(t *testing.T, c *streamCollector, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if _, err := c.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
}

// A terminal event that omits the output field must keep the items the
// stream accumulated — absence is not an empty result.
func TestStreamCollectorKeepsItemsWhenTerminalOmitsOutput(t *testing.T) {
	c := newStreamCollector(nil, nil)
	feedLines(t, c,
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"你好"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"你好世界"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":3,"output_tokens":2}}}`,
	)
	outcome := c.finalize()
	if outcome.Status != "completed" {
		t.Fatalf("status = %q", outcome.Status)
	}
	if len(outcome.Items) != 1 || outcome.Items[0].ID != "msg_1" {
		t.Fatalf("items lost on terminal without output: %+v", outcome.Items)
	}
	if text := messageText(outcome.Items); text != "你好世界" {
		t.Fatalf("text = %q", text)
	}
	if outcome.Usage.Input != 3 || outcome.Usage.Output != 2 || !outcome.Usage.Seen {
		t.Fatalf("usage = %+v", outcome.Usage)
	}

	// An explicit empty output array still replaces: the upstream said there
	// were no items.
	empty := newStreamCollector(nil, nil)
	feedLines(t, empty,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_2","role":"assistant","content":[]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[]}}`,
	)
	outcome = empty.finalize()
	if len(outcome.Items) != 0 {
		t.Fatalf("explicit empty output must replace items: %+v", outcome.Items)
	}
}
