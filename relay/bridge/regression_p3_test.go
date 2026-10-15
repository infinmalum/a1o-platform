package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAnthropicStreamToChatAcceptsStopSequence pins that a stream which hit a
// client-configured stop word completes with finish "stop": the request side
// forwards stop_sequences, so the response side must accept the matching
// stop_sequence report instead of failing mid-stream.
func TestAnthropicStreamToChatAcceptsStopSequence(t *testing.T) {
	fixture := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-upstream\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":3}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"stop_sequence\",\"stop_sequence\":\"END\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	writer := &recordingWriter{}
	usage, err := chatAnthropic{}.Stream(writer, strings.NewReader(fixture), "client-alias")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Complete {
		t.Fatal("stop_sequence stream did not complete")
	}
	converted := writer.buffer.String()
	if !strings.Contains(converted, `"finish_reason":"stop"`) {
		t.Fatalf("stop_sequence stream did not convert to finish stop: %s", converted)
	}
	if strings.Contains(converted, "gateway_stream_error") {
		t.Fatalf("stop_sequence stream emitted an error event: %s", converted)
	}
}

// TestChatClientStreamsEmitErrorEvents pins that every Chat-speaking stream
// converter reports a conversion failure as an error event instead of ending
// the stream silently.
func TestChatClientStreamsEmitErrorEvents(t *testing.T) {
	cases := []struct {
		name string
		body string
		run  func(w *recordingWriter, body string) error
	}{
		{
			name: "anthropic",
			body: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"c\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1}}}\n\nevent: bogus_event\ndata: {\"type\":\"bogus_event\"}\n\n",
			run: func(w *recordingWriter, body string) error {
				_, err := chatAnthropic{}.Stream(w, strings.NewReader(body), "alias")
				return err
			},
		},
		{
			name: "gemini",
			body: "data: {\"candidates\":[{\"index\":1}]}\n\n",
			run: func(w *recordingWriter, body string) error {
				_, err := chatGemini{}.Stream(w, strings.NewReader(body), "alias")
				return err
			},
		},
		{
			name: "responses",
			body: "data: {}\n\n",
			run: func(w *recordingWriter, body string) error {
				_, err := (&chatResponses{}).Stream(w, strings.NewReader(body), "alias")
				return err
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			writer := &recordingWriter{}
			if testCase.run(writer, testCase.body) == nil {
				t.Fatal("expected the stream to fail")
			}
			if !strings.Contains(writer.buffer.String(), `{"error":`) {
				t.Fatalf("stream failed without an error event: %s", writer.buffer.String())
			}
		})
	}
}

// TestAnthropicClientStreamsEmitErrorEvents pins the same guarantee for the
// converters whose clients speak Anthropic Messages: the failure surfaces as
// a terminal error event in the client's own protocol.
func TestAnthropicClientStreamsEmitErrorEvents(t *testing.T) {
	cases := []struct {
		name string
		body string
		run  func(w *recordingWriter, body string) error
	}{
		{
			name: "chat-to-anthropic",
			body: "data: {\"choices\":[{\"index\":1,\"delta\":{}}]}\n\n",
			run: func(w *recordingWriter, body string) error {
				_, err := anthropicChat{}.Stream(w, strings.NewReader(body), "alias")
				return err
			},
		},
		{
			name: "gemini-to-messages",
			body: "data: {\"candidates\":[{\"index\":2}]}\n\n",
			run: func(w *recordingWriter, body string) error {
				_, err := messagesGemini{}.Stream(w, strings.NewReader(body), "alias")
				return err
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			writer := &recordingWriter{}
			if testCase.run(writer, testCase.body) == nil {
				t.Fatal("expected the stream to fail")
			}
			output := writer.buffer.String()
			if !strings.Contains(output, "event: error") || !strings.Contains(output, `"type":"error"`) {
				t.Fatalf("stream failed without an Anthropic error event: %s", output)
			}
			var payload struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			for _, event := range writer.events(t) {
				if strings.HasPrefix(event, `{"type":"error"`) {
					if err := json.Unmarshal([]byte(event), &payload); err != nil {
						t.Fatalf("error event is not valid JSON: %s", event)
					}
					if payload.Error.Message == "" {
						t.Fatalf("error event carries no message: %s", event)
					}
				}
			}
		})
	}
}

// TestChatToAnthropicResponseRejectsMalformedTextPart pins the strict text
// part validation: a non-string text field must fail the conversion instead
// of being silently replaced with empty content.
func TestChatToAnthropicResponseRejectsMalformedTextPart(t *testing.T) {
	input := []byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"gpt","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":123}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	if _, _, err := ChatToAnthropicResponse(input, "alias"); err == nil {
		t.Fatal("malformed text part was silently converted to empty content")
	}
}

// TestResponsesRequestRejectsMalformedTextPart pins the same strictness on the
// Responses request path, which used to swallow the error via a lossy helper.
func TestResponsesRequestRejectsMalformedTextPart(t *testing.T) {
	input := []byte(`{"model":"alias","messages":[{"role":"system","content":[{"type":"text","text":true}]}],"max_tokens":10}`)
	if _, err := (&chatResponses{}).Request(input, "alias"); err == nil {
		t.Fatal("malformed system text part was silently converted to empty text")
	}
}
