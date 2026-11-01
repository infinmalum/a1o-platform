package wire

import (
	"encoding/json"
	"github.com/infinmalum/a1o-platform/relay/native"
	"github.com/infinmalum/a1o-platform/relay/sse"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamingLifecycle(t *testing.T) {
	for _, protocol := range []native.Protocol{native.Anthropic, native.OpenAIResponses, native.OpenAIChat} {
		w := httptest.NewRecorder()
		s := NewSession(w, protocol, "id", "model")
		if err := s.Delta("hello"); err != nil {
			t.Fatal(err)
		}
		if !w.Flushed {
			t.Fatal("delta was buffered")
		}
		if err := s.Complete(Usage{Input: 2, Output: 3}); err != nil {
			t.Fatal(err)
		}
		var kinds []string
		err := sse.Read(strings.NewReader(w.Body.String()), func(e sse.Event) error {
			if e.Data == "[DONE]" {
				return nil
			}
			var p map[string]any
			if err := json.Unmarshal([]byte(e.Data), &p); err != nil {
				return err
			}
			if kind, ok := p["type"].(string); ok {
				kinds = append(kinds, kind)
				if protocol == native.OpenAIResponses && e.Name != kind {
					t.Fatal("missing named event")
				}
			}
			if p["type"] == "response.completed" {
				raw, _ := json.Marshal(p["response"])
				if !strings.Contains(string(raw), `"text":"hello"`) {
					t.Fatal(string(raw))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(kinds, ",")
		if protocol == native.Anthropic && joined != "message_start,content_block_start,content_block_delta,content_block_stop,message_delta,message_stop" {
			t.Fatal(joined)
		}
		if protocol == native.OpenAIResponses && !strings.Contains(joined, "response.output_text.done,response.content_part.done,response.output_item.done,response.completed") {
			t.Fatal(joined)
		}
	}
}
