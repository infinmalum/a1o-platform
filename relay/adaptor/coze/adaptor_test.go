package coze

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/relay/meta"
	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// TestConvertRequestFromEachWire locks the direct-conversion rule: the Chat,
// Messages, and Responses wires each build the v3 request from their own
// body, with no wire routed through another.
func TestConvertRequestFromEachWire(t *testing.T) {
	chat := &relaymodel.ConversionInput{Request: &relaymodel.TextRequest{}}
	chat.Request.Messages = []relaymodel.Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi"},
	}
	converted, err := ConvertRequest(chat, "bot-1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if converted.BotID != "bot-1" || converted.UserID != "user-1" || !converted.Stream || !converted.AutoSaveHistory {
		t.Fatalf("v3 head is wrong: %+v", converted)
	}
	// The system turn folds into a user turn: the wire has no system role.
	if len(converted.AdditionalMsgs) != 2 || converted.AdditionalMsgs[0].Role != "user" ||
		converted.AdditionalMsgs[0].Content != "be brief" || converted.AdditionalMsgs[1].Role != "user" {
		t.Fatalf("chat messages converted wrong: %+v", converted.AdditionalMsgs)
	}

	anthropic := &relaymodel.ConversionInput{Inbound: native.Anthropic,
		RawBody: json.RawMessage(`{"system":"be brief","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)}
	converted, err = ConvertRequest(anthropic, "bot-2", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(converted.AdditionalMsgs) != 2 || converted.AdditionalMsgs[1].Content != "hi" {
		t.Fatalf("anthropic messages converted wrong: %+v", converted.AdditionalMsgs)
	}

	responses := &relaymodel.ConversionInput{Inbound: native.OpenAIResponses,
		RawBody: json.RawMessage(`{"instructions":"be brief","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)}
	converted, err = ConvertRequest(responses, "bot-3", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(converted.AdditionalMsgs) != 2 {
		t.Fatalf("responses messages converted wrong: %+v", converted.AdditionalMsgs)
	}

	// Richer content fails loudly instead of being dropped.
	toolCall := &relaymodel.ConversionInput{Inbound: native.Anthropic,
		RawBody: json.RawMessage(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"f","input":{}}]}]}`)}
	if _, err := ConvertRequest(toolCall, "b", "u"); err == nil {
		t.Fatal("tool_use block was not rejected")
	}
}

// TestHandleResponseConvertsEachWire drives the event-stream reader and the
// per-wire writers through an in-memory response.
func TestHandleResponseConvertsEachWire(t *testing.T) {
	stream := "event: conversation.chat.created\ndata: {\"id\":\"chat-9\",\"status\":\"created\"}\n\n" +
		"event: conversation.message.delta\ndata: {\"id\":\"m1\",\"role\":\"assistant\",\"type\":\"answer\",\"content\":\"he\"}\n\n" +
		"event: conversation.message.delta\ndata: {\"id\":\"m1\",\"role\":\"assistant\",\"type\":\"answer\",\"content\":\"llo\"}\n\n" +
		"event: conversation.chat.completed\ndata: {\"id\":\"chat-9\",\"status\":\"completed\",\"usage\":{\"token_count\":9,\"output_count\":3,\"input_count\":6}}\n\n" +
		"event: done\ndata: \"[DONE]\"\n\n"
	for _, tc := range []struct {
		inbound native.Protocol
		stream  bool
		marker  string
	}{
		{native.OpenAIChat, true, `"content":"llo"`},
		{native.OpenAIChat, false, `"content":"hello"`},
		{native.Anthropic, true, "text_delta"},
		{native.Anthropic, false, `"text":"hello"`},
		{native.OpenAIResponses, true, `"delta":"llo"`},
		{native.OpenAIResponses, false, `"text":"hello"`},
	} {
		requestMeta := &meta.Meta{Inbound: tc.inbound, IsStream: tc.stream, ActualModelName: "bot"}
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(stream))}
		recorder := httptest.NewRecorder()
		prompt, completion, err := HandleResponse(recorder, resp, requestMeta)
		if err != nil {
			t.Fatalf("%s stream=%v: %v", tc.inbound, tc.stream, err)
		}
		if prompt != 6 || completion != 3 {
			t.Fatalf("%s usage lost: %d/%d", tc.inbound, prompt, completion)
		}
		if !strings.Contains(recorder.Body.String(), tc.marker) {
			t.Fatalf("%s stream=%v output lost the answer: %s", tc.inbound, tc.stream, recorder.Body.String())
		}
	}
}

func TestDoneDoesNotHideMissingCompletion(t *testing.T) {
	r := &cozeResult{}
	if err := r.absorb(StreamEvent{Event: "done", Data: []byte(`"[DONE]"`)}); err == nil {
		t.Fatal("missing completion accepted")
	}
	if err := r.absorb(StreamEvent{Event: "conversation.chat.failed", Data: []byte(`{"last_error":{"code":1,"msg":"failed explicitly"}}`)}); err != nil {
		t.Fatal(err)
	}
	if r.failure != "failed explicitly" {
		t.Fatal(r.failure)
	}
}
