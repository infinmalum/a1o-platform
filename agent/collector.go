package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// streamCollector is the http.ResponseWriter side of one model call: the
// lifecycle writes the client-protocol (Responses) SSE into it and it
// reassembles standard Items, usage, and the terminal status. Nothing here
// touches the network or billing; lifecycle owns both.
type streamCollector struct {
	buf        []byte
	responseID string
	status     string
	failure    error
	complete   bool // a terminal response.* event arrived

	// items is the ordered output. Open items mutate in place as deltas
	// arrive; output_item.done replaces the entry with the authoritative
	// snapshot, and a terminal response.output replaces the whole list.
	items []Item

	usage native.Usage

	// onTextDelta and onReasoningDelta stream increments to the caller
	// (Engine UI projection in R2); nil in storage-only runs.
	onTextDelta      func(string)
	onReasoningDelta func(string)
}

func newStreamCollector(onTextDelta, onReasoningDelta func(string)) *streamCollector {
	return &streamCollector{onTextDelta: onTextDelta, onReasoningDelta: onReasoningDelta}
}

// Header and WriteHeader satisfy http.ResponseWriter; lifecycle writes
// response headers through them and they carry no meaning here.
func (c *streamCollector) Header() http.Header { return http.Header{} }
func (c *streamCollector) WriteHeader(int)     {}
func (c *streamCollector) Flush()              {}

func (c *streamCollector) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	for {
		idx := bytes.IndexByte(c.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSuffix(string(c.buf[:idx]), "\r")
		c.buf = c.buf[idx+1:]
		c.line(line)
	}
	return len(p), nil
}

func (c *streamCollector) line(line string) {
	if c.complete || c.failure != nil || !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}
	var event struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		c.failure = fmt.Errorf("malformed Responses stream event: %w", err)
		return
	}
	if err := c.event(event.Type, []byte(payload)); err != nil {
		c.failure = err
	}
}

type streamEnvelope struct {
	Response struct {
		ID     string            `json:"id"`
		Status string            `json:"status"`
		Output []json.RawMessage `json:"output"`
		Usage  *streamUsage      `json:"usage"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
	Item json.RawMessage `json:"item"`
	// Delta events
	ItemID string `json:"item_id"`
	Delta  string `json:"delta"`
}

type streamUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func (u *streamUsage) native() native.Usage {
	usage := native.Usage{Input: u.InputTokens, Output: u.OutputTokens, Seen: true}
	if u.InputTokensDetails != nil {
		usage.CacheRead = u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil {
		usage.Reasoning = u.OutputTokensDetails.ReasoningTokens
	}
	return usage
}

func (c *streamCollector) event(eventType string, payload []byte) error {
	var envelope streamEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("malformed %s event: %w", eventType, err)
	}
	switch eventType {
	case "response.created":
		c.responseID = envelope.Response.ID
	case "response.output_item.added":
		item, err := parseItem(envelope.Item)
		if err != nil {
			return err
		}
		if item.ID == "" {
			return errors.New("output_item.added carries no item id")
		}
		item.Status = ItemStatusInProgress
		c.items = append(c.items, item)
	case "response.reasoning_summary_text.delta":
		if c.onReasoningDelta != nil {
			c.onReasoningDelta(envelope.Delta)
		}
		c.addItemDelta(envelope.ItemID, func(item *Item) {
			if len(item.Summary) == 0 {
				item.Summary = []SummaryPart{{Type: PartSummaryText}}
			}
			item.Summary[0].Text += envelope.Delta
		})
	case "response.content_part.added", "response.content_part.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done",
		"response.reasoning_summary_text.done":
		// Part boundaries and done snapshots carry no payload the item
		// snapshot does not.
	case "response.output_text.delta":
		if c.onTextDelta != nil {
			c.onTextDelta(envelope.Delta)
		}
		c.addItemDelta(envelope.ItemID, func(item *Item) {
			item.Content = appendOrExtend(item.Content, PartOutputText, envelope.Delta, "")
		})
	case "response.refusal.delta":
		c.addItemDelta(envelope.ItemID, func(item *Item) {
			item.Content = appendOrExtend(item.Content, PartRefusal, envelope.Delta, "")
		})
	case "response.output_text.done", "response.refusal.done", "response.function_call_arguments.done":
		// Done events for parts carry snapshots; output_item.done is
		// authoritative and follows.
	case "response.function_call_arguments.delta":
		c.addItemDelta(envelope.ItemID, func(item *Item) {
			item.Arguments += envelope.Delta
		})
	case "response.output_item.done":
		item, err := parseItem(envelope.Item)
		if err != nil {
			return err
		}
		c.replaceItem(item)
	case "response.completed", "response.incomplete":
		items := make([]Item, 0, len(envelope.Response.Output))
		for _, raw := range envelope.Response.Output {
			item, err := parseItem(raw)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		c.items = items
		c.status = envelope.Response.Status
		c.responseID = envelope.Response.ID
		if envelope.Response.Usage != nil {
			c.usage = envelope.Response.Usage.native()
		}
		c.complete = true
	case "response.failed":
		c.status = ItemStatusFailed
		c.complete = true
		if envelope.Response.Error != nil {
			return fmt.Errorf("model stream failed: %s", envelope.Response.Error.Message)
		}
		return errors.New("model stream failed")
	case "error":
		var streamError struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(payload, &streamError); err != nil {
			return fmt.Errorf("model stream error event: %s", payload)
		}
		return fmt.Errorf("model stream error (%s): %s", streamError.Code, streamError.Message)
	default:
		return fmt.Errorf("Responses stream event %q cannot be interpreted", eventType)
	}
	return nil
}

// addItemDelta routes a delta to the open item with the given id.
func (c *streamCollector) addItemDelta(itemID string, apply func(*Item)) {
	for i := range c.items {
		if c.items[i].ID == itemID {
			apply(&c.items[i])
			return
		}
	}
}

// replaceItem upserts an authoritative done snapshot by item id.
func (c *streamCollector) replaceItem(item Item) {
	for i := range c.items {
		if c.items[i].ID == item.ID {
			c.items[i] = item
			return
		}
	}
	c.items = append(c.items, item)
}

func appendOrExtend(parts []ContentPart, partType, text, refusal string) []ContentPart {
	if len(parts) > 0 && parts[len(parts)-1].Type == partType {
		last := &parts[len(parts)-1]
		last.Text += text
		last.Refusal += refusal
		return parts
	}
	return append(parts, ContentPart{Type: partType, Text: text, Refusal: refusal})
}

// parseItem converts one Responses output item into the standard Item.
// Unknown item types fail loudly: passing them through would pretend support
// the runtime does not have.
func parseItem(raw json.RawMessage) (Item, error) {
	var item Item
	if len(raw) == 0 {
		return item, errors.New("output item is empty")
	}
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return item, fmt.Errorf("malformed output item: %w", err)
	}
	item.Type = header.Type
	switch header.Type {
	case ItemMessage:
		var wire struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Role    string `json:"role"`
			Content []struct {
				Type        string           `json:"type"`
				Text        string           `json:"text"`
				Refusal     string           `json:"refusal"`
				Annotations []map[string]any `json:"annotations"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			return item, fmt.Errorf("malformed message item: %w", err)
		}
		item.ID, item.Status, item.Role = wire.ID, wire.Status, wire.Role
		for _, part := range wire.Content {
			switch part.Type {
			case PartOutputText, PartRefusal:
				item.Content = append(item.Content, ContentPart{
					Type: part.Type, Text: part.Text, Refusal: part.Refusal, Annotations: part.Annotations,
				})
			default:
				return item, fmt.Errorf("message content part %q cannot be represented", part.Type)
			}
		}
	case ItemReasoning:
		var wire struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Summary []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"summary"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			EncryptedContent string `json:"encrypted_content"`
			Signature        string `json:"signature"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			return item, fmt.Errorf("malformed reasoning item: %w", err)
		}
		item.ID, item.Status = wire.ID, wire.Status
		for _, entry := range wire.Summary {
			if entry.Type != "" && entry.Type != PartSummaryText {
				return item, fmt.Errorf("reasoning summary part %q cannot be represented", entry.Type)
			}
			item.Summary = append(item.Summary, SummaryPart{Type: PartSummaryText, Text: entry.Text})
		}
		for _, entry := range wire.Content {
			if entry.Type != "" && entry.Type != PartReasoningText {
				return item, fmt.Errorf("reasoning content part %q cannot be represented", entry.Type)
			}
			item.Content = append(item.Content, ContentPart{Type: PartReasoningText, Text: entry.Text})
		}
		item.EncryptedContent = wire.EncryptedContent
		item.Signature = wire.Signature
	case ItemFunctionCall:
		var wire struct {
			ID               string `json:"id"`
			Status           string `json:"status"`
			CallID           string `json:"call_id"`
			Name             string `json:"name"`
			Arguments        string `json:"arguments"`
			ThoughtSignature string `json:"thought_signature"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			return item, fmt.Errorf("malformed function_call item: %w", err)
		}
		item.ID, item.Status = wire.ID, wire.Status
		item.CallID, item.Name, item.Arguments = wire.CallID, wire.Name, wire.Arguments
		item.ThoughtSignature = wire.ThoughtSignature
	case ItemFunctionCallOutput:
		var wire struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			return item, fmt.Errorf("malformed function_call_output item: %w", err)
		}
		item.ID, item.Status = wire.ID, wire.Status
		item.CallID, item.Output = wire.CallID, wire.Output
	default:
		return item, fmt.Errorf("output item type %q cannot be represented", header.Type)
	}
	return item, nil
}

// streamOutcome is the collector's final read.
type streamOutcome struct {
	ResponseID string
	Status     string // completed | incomplete | failed | "" when no terminal arrived
	Items      []Item
	Usage      native.Usage
	Err        error
}

func (c *streamCollector) finalize() streamOutcome {
	return streamOutcome{
		ResponseID: c.responseID,
		Status:     c.status,
		Items:      c.items,
		Usage:      c.usage,
		Err:        c.failure,
	}
}
