package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/relay/lifecycle"
	"github.com/infinmalum/one-gateway/relay/native"
)

// LegRequest carries one model call.
type LegRequest struct {
	// Body is an OpenAI chat completions body; stream is always true.
	Body []byte
	// MaxOutputTokens mirrors body.max_tokens for quota reservation.
	MaxOutputTokens int64
}

// LegToolCall is one tool invocation requested by the model.
type LegToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// LegResult is the accumulated outcome of one model leg.
type LegResult struct {
	Content          string
	ToolCalls        []LegToolCall
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
}

// LegRunner executes one model call, streaming assistant text deltas through
// onDelta while accumulating the full result.
type LegRunner interface {
	RunLeg(ctx context.Context, req LegRequest, onDelta func(string)) (LegResult, error)
}

// NewLifecycleLeg returns a LegRunner that executes model calls through the
// shared relay lifecycle, so built-in chat requests follow the same channel
// selection, protocol bridging, billing, and logging as /v1 requests.
func NewLifecycleLeg(modelName string, userID, tokenID int, tokenName string) LegRunner {
	return &lifecycleLeg{Model: modelName, UserID: userID, TokenID: tokenID, TokenName: tokenName}
}

// lifecycleLeg streams one chat completion through lifecycle.Forward into the
// SSE parser below, which doubles as the ResponseWriter.
type lifecycleLeg struct {
	Model     string
	UserID    int
	TokenID   int
	TokenName string
}

func (l *lifecycleLeg) RunLeg(ctx context.Context, req LegRequest, onDelta func(string)) (LegResult, error) {
	parser := newChatStreamParser(onDelta)
	input := lifecycle.Request{
		Protocol:        native.OpenAIChat,
		Model:           l.Model,
		Stream:          true,
		MaxOutputTokens: req.MaxOutputTokens,
		Body:            req.Body,
		Headers:         http.Header{},
		Principal:       lifecycle.Principal{UserID: l.UserID, TokenID: l.TokenID, TokenName: l.TokenName},
		RetryLimit:      config.RetryTimes,
	}
	if herr := lifecycle.Forward(ctx, parser, input); herr != nil {
		// Nothing reached the client; surface the lifecycle failure verbatim.
		result, finErr := parser.finalize()
		_ = finErr
		return result, errors.New(herr.Message)
	}
	return parser.finalize()
}

// chatToolDelta is one incremental tool-call fragment within a chunk.
type chatToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatChunk mirrors the parts of an OpenAI chat completion chunk the runtime
// consumes.
type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []chatToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// chatStreamParser reassembles the OpenAI chat completions SSE that the
// lifecycle writes in the client protocol. It accumulates content, tool-call
// deltas by index, finish reason, and usage.
type chatStreamParser struct {
	onDelta    func(string)
	buf        []byte
	result     LegResult
	toolBySlot map[int]*chatToolDelta
	toolOrder  []int
	failure    error
	done       bool
}

func newChatStreamParser(onDelta func(string)) *chatStreamParser {
	return &chatStreamParser{onDelta: onDelta, toolBySlot: map[int]*chatToolDelta{}}
}

// Header and WriteHeader satisfy http.ResponseWriter; the lifecycle writes
// the client-protocol response headers through them and they carry no
// meaning here.
func (p *chatStreamParser) Header() http.Header { return http.Header{} }
func (p *chatStreamParser) WriteHeader(int)     {}
func (p *chatStreamParser) Flush()              {}

func (p *chatStreamParser) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	for {
		idx := bytes.IndexByte(p.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSuffix(string(p.buf[:idx]), "\r")
		p.buf = p.buf[idx+1:]
		p.line(line)
	}
	return len(b), nil
}

func (p *chatStreamParser) line(line string) {
	if p.done || p.failure != nil || !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return
	}
	if payload == "[DONE]" {
		p.done = true
		return
	}
	var chunk chatChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return
	}
	if chunk.Error != nil {
		p.failure = errors.New(chunk.Error.Message)
		return
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			p.result.Content += choice.Delta.Content
			if p.onDelta != nil {
				p.onDelta(choice.Delta.Content)
			}
		}
		for i := range choice.Delta.ToolCalls {
			p.toolDelta(choice.Delta.ToolCalls[i])
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			p.result.FinishReason = *choice.FinishReason
		}
	}
	if chunk.Usage != nil {
		p.result.PromptTokens = chunk.Usage.PromptTokens
		p.result.CompletionTokens = chunk.Usage.CompletionTokens
	}
}

func (p *chatStreamParser) toolDelta(call chatToolDelta) {
	slot, ok := p.toolBySlot[call.Index]
	if !ok {
		slot = &chatToolDelta{}
		p.toolBySlot[call.Index] = slot
		p.toolOrder = append(p.toolOrder, call.Index)
	}
	if call.ID != "" {
		slot.ID = call.ID
	}
	if call.Function.Name != "" {
		slot.Name = call.Function.Name
	} else if call.Name != "" {
		slot.Name = call.Name
	}
	slot.Function.Arguments += call.Function.Arguments
}

// finalize returns the accumulated result, assigning generated IDs to tool
// calls whose provider omitted them.
func (p *chatStreamParser) finalize() (LegResult, error) {
	for i, slotIndex := range p.toolOrder {
		slot := p.toolBySlot[slotIndex]
		if slot.ID == "" {
			slot.ID = fmt.Sprintf("call_%d", i+1)
		}
		p.result.ToolCalls = append(p.result.ToolCalls, LegToolCall{
			ID: slot.ID, Name: slot.Name, Arguments: slot.Function.Arguments,
		})
	}
	return p.result, p.failure
}
