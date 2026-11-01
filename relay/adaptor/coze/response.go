package coze

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/sse"
	"github.com/infinmalum/a1o-platform/relay/wire"
)

// HandleResponse converts the Coze v3 event stream onto the client's wire.
// The upstream always streams; a synchronous client receives the aggregated
// result as one JSON body in its own protocol's shape.
func HandleResponse(w http.ResponseWriter, resp *http.Response, requestMeta *meta.Meta) (promptTokens, completionTokens int64, err error) {
	defer resp.Body.Close()
	state := &cozeResult{messages: make(map[string]string)}
	var session *wire.Session
	err = sse.Read(resp.Body, func(event sse.Event) error {
		before := state.text.Len()
		if e := state.absorb(StreamEvent{Event: event.Name, Data: []byte(event.Data)}); e != nil {
			return e
		}
		if state.failed {
			return errors.New("Coze chat failed: " + state.failure)
		}
		if requestMeta.IsStream && state.text.Len() > before {
			if session == nil {
				session = wire.NewSession(w, requestMeta.Inbound, state.id, requestMeta.ActualModelName)
			}
			if e := session.Delta(state.text.String()[before:]); e != nil {
				return e
			}
		}
		if state.complete {
			return io.EOF
		}
		return nil
	})
	if err == nil && !state.complete {
		err = errors.New("Coze stream ended before conversation.chat.completed")
	}
	if err != nil {
		return state.inputTokens, state.outputTokens, err
	}
	usage := wire.Usage{Input: state.inputTokens, Output: state.outputTokens}
	if session != nil {
		err = session.Complete(usage)
	} else {
		err = wire.Write(w, requestMeta.Inbound, state.id, requestMeta.ActualModelName, state.text.String(), usage, requestMeta.IsStream)
	}
	return state.inputTokens, state.outputTokens, err
}

// cozeResult accumulates the event stream into the answer text and the usage
// report, so both the streaming and aggregated writers render one truth.
type cozeResult struct {
	id           string
	text         strings.Builder
	complete     bool
	failed       bool
	failure      string
	inputTokens  int64
	outputTokens int64
	messages     map[string]string
}

func (r *cozeResult) absorb(event StreamEvent) error {
	switch event.Event {
	case "conversation.chat.created", "conversation.chat.in_progress":
		var payload chatEvent
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return errors.New("invalid Coze chat event")
		}
		if payload.ID != "" {
			r.id = payload.ID
		}
	case "conversation.message.delta":
		var payload messageEvent
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return errors.New("invalid Coze message event")
		}
		if payload.Type != messageTypeAnswer {
			// verbose and follow_up are bot metadata, not answer content;
			// any other type carries content this gateway cannot place and
			// fails the stream instead of dropping it.
			if payload.Type != messageTypeVerbose && payload.Type != messageTypeFollowUp {
				return fmt.Errorf("Coze conversion cannot represent the %q message type", payload.Type)
			}
			return nil
		}
		if r.messages == nil {
			r.messages = make(map[string]string)
		}
		r.messages[payload.ID] += payload.Content
		r.text.WriteString(payload.Content)
	case "conversation.message.completed":
		var payload messageEvent
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		if payload.Type == messageTypeAnswer && r.messages[payload.ID] == "" {
			if r.messages == nil {
				r.messages = make(map[string]string)
			}
			r.messages[payload.ID] = payload.Content
			r.text.WriteString(payload.Content)
		}
	case "conversation.chat.completed":
		var payload chatEvent
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return errors.New("invalid Coze chat event")
		}
		r.complete = true
		if payload.Usage != nil {
			r.inputTokens, r.outputTokens = payload.Usage.InputCount, payload.Usage.OutputCount
		}
	case "conversation.chat.failed", "error":
		message := cozeFailureMessage(event.Data)
		r.failed = true
		r.failure = message
	case "conversation.chat.requires_action":
		return fmt.Errorf("Coze bot requires external tool outputs; this gateway cannot resume that action")
	case "done":
		if !r.complete {
			return errors.New("Coze done without completed chat")
		}
	default:
		// Providers may add metadata events; only explicit chat completion ends a run.
	}
	return nil
}

// cozeFailureMessage reads the failure payload, whose shape differs between
// the chat.failed chat object and the bare error event.
func cozeFailureMessage(data []byte) string {
	var chat chatEvent
	if json.Unmarshal(data, &chat) == nil {
		if chat.Msg != "" {
			return chat.Msg
		}
		if chat.LastError != nil && chat.LastError.Msg != "" {
			return chat.LastError.Msg
		}
		if chat.LastErrorMessage != "" {
			return chat.LastErrorMessage
		}
	}
	var failure errorEvent
	if json.Unmarshal(data, &failure) == nil && failure.Msg != "" {
		return failure.Msg
	}
	return "upstream Coze stream failed"
}

// readCozeEvents is used by framing tests; production consumes incrementally.
func readCozeEvents(body io.Reader) ([]StreamEvent, error) {
	var events []StreamEvent
	err := sse.Read(body, func(e sse.Event) error {
		events = append(events, StreamEvent{Event: e.Name, Data: []byte(e.Data)})
		return nil
	})
	return events, err
}
