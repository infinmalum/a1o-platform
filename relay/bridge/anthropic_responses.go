package bridge

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// anthropicResponses converts the Anthropic Messages client wire onto OpenAI
// Responses upstreams. Messages is the richer wire, so the request conversion
// is a projection: stop_sequences has no Responses representation and is
// rejected before the upstream call. Thinking round-trips: Claude thinking
// blocks map onto reasoning items with their signature, redacted thinking onto
// the encrypted_content slot. It is the reverse of responsesAnthropic.
type anthropicResponses struct{}

// Request converts the representable Messages subset onto the Responses wire.
func (anthropicResponses) Request(body []byte, model string) ([]byte, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	fields, err := object(body, "model", "messages", "max_tokens", "system", "stream",
		"temperature", "top_p", "tools", "tool_choice")
	if err != nil {
		return nil, err
	}
	var stream bool
	if err := decode(fields["stream"], &stream); err != nil {
		return nil, errors.New("stream must be boolean")
	}
	var limit int64
	if err := json.Unmarshal(fields["max_tokens"], &limit); err != nil || limit <= 0 {
		return nil, errors.New("positive max_tokens is required")
	}
	request := map[string]any{"model": model, "max_output_tokens": limit}
	if stream {
		request["stream"] = true
	}
	var instructions []string
	var input []any
	if len(fields["system"]) != 0 {
		text, err := messagesText(fields["system"])
		if err != nil {
			return nil, fmt.Errorf("system: %w", err)
		}
		instructions = append(instructions, text)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(fields["messages"], &messages); err != nil || len(messages) == 0 {
		return nil, errors.New("messages must be a non-empty array")
	}
	for _, raw := range messages {
		message, err := object(raw, "role", "content")
		if err != nil {
			return nil, err
		}
		var role string
		if err := json.Unmarshal(message["role"], &role); err != nil || (role != "user" && role != "assistant") {
			return nil, errors.New("Messages role must be user or assistant")
		}
		items, err := messagesBlocksToResponses(message["content"], role)
		if err != nil {
			return nil, err
		}
		input = append(input, items...)
	}
	if len(input) == 0 {
		return nil, errors.New("Messages content produced no convertible input")
	}
	request["input"] = input
	if len(instructions) > 0 {
		request["instructions"] = joinSystem(instructions)
	}
	for _, key := range []string{"temperature", "top_p"} {
		if len(fields[key]) != 0 {
			request[key] = json.RawMessage(fields[key])
		}
	}
	if len(fields["tools"]) != 0 {
		var tools []json.RawMessage
		if json.Unmarshal(fields["tools"], &tools) != nil {
			return nil, errors.New("tools must be an array")
		}
		var converted []any
		for _, toolRaw := range tools {
			tool, err := object(toolRaw, "name", "description", "input_schema")
			if err != nil {
				return nil, err
			}
			var name string
			_ = json.Unmarshal(tool["name"], &name)
			if name == "" {
				return nil, errors.New("client tool requires a name")
			}
			function := map[string]any{"type": "function", "name": name}
			if len(tool["description"]) != 0 {
				function["description"] = json.RawMessage(tool["description"])
			}
			if len(tool["input_schema"]) != 0 && string(tool["input_schema"]) != "null" {
				var schema map[string]any
				if json.Unmarshal(tool["input_schema"], &schema) != nil || schema == nil {
					return nil, errors.New("client tool input_schema must be a JSON object")
				}
				function["parameters"] = json.RawMessage(tool["input_schema"])
			}
			converted = append(converted, function)
		}
		request["tools"] = converted
	}
	if len(fields["tool_choice"]) != 0 {
		choice, err := anthropicToolChoiceToResponses(fields["tool_choice"])
		if err != nil {
			return nil, err
		}
		request["tool_choice"] = choice
	}
	return json.Marshal(request)
}

// messagesBlocksToResponses converts one Messages message onto Responses input
// items. Thinking blocks map onto reasoning items carrying their signature;
// redacted thinking rides the encrypted_content slot so the opaque blob
// returns to the vendor that issued it. tool_result error flags travel as an
// is_error extension on the function_call_output item.
func messagesBlocksToResponses(raw json.RawMessage, role string) ([]any, error) {
	var plain string
	if len(raw) != 0 && json.Unmarshal(raw, &plain) == nil {
		if plain == "" {
			return nil, errors.New("Messages content is empty")
		}
		kind := "input_text"
		if role == "assistant" {
			kind = "output_text"
		}
		return []any{responsesMessageInputItem(role, []any{map[string]string{"type": kind, "text": plain}})}, nil
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return nil, errors.New("Messages content must be text or a non-empty block array")
	}
	var items []any
	var pendingParts []any
	flushText := func() {
		if len(pendingParts) > 0 {
			items = append(items, responsesMessageInputItem(role, pendingParts))
			pendingParts = nil
		}
	}
	for _, blockRaw := range blocks {
		block, err := object(blockRaw, "type", "text", "source", "id", "name", "input", "tool_use_id", "content", "is_error", "thinking", "signature", "data")
		if err != nil {
			return nil, err
		}
		switch rawString(block["type"]) {
		case "text":
			if len(block) != 2 {
				return nil, errors.New("text block has fields that Responses cannot represent")
			}
			var text string
			if json.Unmarshal(block["text"], &text) != nil {
				return nil, errors.New("text block text must be a string")
			}
			kind := "input_text"
			if role == "assistant" {
				kind = "output_text"
			}
			pendingParts = append(pendingParts, map[string]string{"type": kind, "text": text})
		case "thinking":
			if role != "assistant" {
				return nil, errors.New("thinking blocks belong to assistant messages")
			}
			var text string
			if json.Unmarshal(block["thinking"], &text) != nil || text == "" {
				return nil, errors.New("thinking block requires thinking text")
			}
			flushText()
			item := map[string]any{"type": "reasoning",
				"summary": []any{map[string]string{"type": "summary_text", "text": text}}}
			if signature := rawString(block["signature"]); signature != "" {
				item["signature"] = signature
			}
			items = append(items, item)
		case "redacted_thinking":
			if role != "assistant" {
				return nil, errors.New("thinking blocks belong to assistant messages")
			}
			data := rawString(block["data"])
			if data == "" {
				return nil, errors.New("redacted thinking requires data")
			}
			flushText()
			items = append(items, map[string]any{"type": "reasoning", "encrypted_content": data})
		case "image":
			if role != "user" || len(block) != 2 {
				return nil, errors.New("image must be a user block with only a source")
			}
			part, err := anthropicImageToResponsesInput(block["source"])
			if err != nil {
				return nil, err
			}
			pendingParts = append(pendingParts, part)
		case "tool_use":
			if role != "assistant" || len(block) != 4 {
				return nil, errors.New("tool_use must be an assistant block with id, name, and input")
			}
			id, name := rawString(block["id"]), rawString(block["name"])
			var input map[string]any
			if id == "" || name == "" || json.Unmarshal(block["input"], &input) != nil || input == nil {
				return nil, errors.New("tool_use requires an object input")
			}
			arguments, err := json.Marshal(input)
			if err != nil {
				return nil, err
			}
			flushText()
			items = append(items, map[string]any{"type": "function_call", "call_id": id, "name": name, "arguments": string(arguments)})
		case "tool_result":
			if role != "user" || len(block) != 3 {
				return nil, errors.New("tool_result cannot be represented in this message")
			}
			id := rawString(block["tool_use_id"])
			content, err := messagesText(block["content"])
			if id == "" || err != nil {
				return nil, errors.New("tool_result requires a tool_use_id and text content")
			}
			flushText()
			output := map[string]any{"type": "function_call_output", "call_id": id, "output": content}
			if nonNull(block["is_error"]) {
				var isError bool
				if json.Unmarshal(block["is_error"], &isError) != nil {
					return nil, errors.New("tool_result is_error must be boolean")
				}
				if isError {
					output["is_error"] = true
				}
			}
			items = append(items, output)
		default:
			return nil, fmt.Errorf("Messages content block %q cannot be converted", rawString(block["type"]))
		}
	}
	flushText()
	return items, nil
}

func responsesMessageInputItem(role string, content []any) map[string]any {
	if role == "system" {
		role = "user"
	}
	return map[string]any{"type": "message", "role": role, "content": content}
}

// anthropicImageToResponsesInput converts a Messages image source onto an
// input_image part carrying a data or remote URL.
func anthropicImageToResponsesInput(raw json.RawMessage) (map[string]any, error) {
	source, err := object(raw, "type", "url", "media_type", "data")
	if err != nil {
		return nil, err
	}
	var address string
	switch rawString(source["type"]) {
	case "url":
		if len(source) != 2 {
			return nil, errors.New("Anthropic URL image requires only a URL")
		}
		address = rawString(source["url"])
	case "base64":
		var mediaType, data string
		if len(source) != 3 || json.Unmarshal(source["media_type"], &mediaType) != nil || json.Unmarshal(source["data"], &data) != nil || !supportedImageType(mediaType) || data == "" {
			return nil, errors.New("Anthropic base64 image requires a supported media type and data")
		}
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			return nil, errors.New("Anthropic image data must be base64")
		}
		address = "data:" + mediaType + ";base64," + data
	default:
		return nil, fmt.Errorf("Anthropic image source %q cannot be converted", rawString(source["type"]))
	}
	if address == "" {
		return nil, errors.New("Anthropic image requires a URL")
	}
	return map[string]any{"type": "input_image", "image_url": address}, nil
}

// anthropicToolChoiceToResponses converts the Messages tool_choice values.
func anthropicToolChoiceToResponses(raw json.RawMessage) (any, error) {
	choice, err := object(raw, "type", "name")
	if err != nil {
		return nil, err
	}
	switch rawString(choice["type"]) {
	case "auto", "none":
		if len(choice) != 1 {
			return nil, errors.New("tool_choice has unsupported fields")
		}
		return rawString(choice["type"]), nil
	case "any":
		if len(choice) != 1 {
			return nil, errors.New("tool_choice has unsupported fields")
		}
		return "required", nil
	case "tool":
		var name string
		_ = json.Unmarshal(choice["name"], &name)
		if name == "" || len(choice) != 2 {
			return nil, errors.New("named tool_choice requires a name")
		}
		return map[string]string{"type": "function", "name": name}, nil
	default:
		return nil, fmt.Errorf("tool_choice %q cannot be converted", rawString(choice["type"]))
	}
}

// Response converts a non-streaming Responses response onto the Messages wire.
// Reasoning items map onto thinking blocks with their signature, and encrypted
// reasoning onto redacted_thinking, so both round-trip.
func (anthropicResponses) Response(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	fields, extras, err := responseObject(body, "id", "status", "error", "incomplete_details", "output", "usage", "created_at", "provider_metadata")
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata := newProviderMetadata(extras)
	var id string
	_ = json.Unmarshal(fields["id"], &id)
	if id == "" {
		generated, err := newStreamID(anthropicMessageIDPrefix)
		if err != nil {
			return nil, native.Usage{}, err
		}
		id = generated
	}
	var output []json.RawMessage
	if json.Unmarshal(fields["output"], &output) != nil {
		return nil, native.Usage{}, errors.New("Responses output must be an array")
	}
	blocks := []any{}
	sawTool := false
	for _, itemRaw := range output {
		item, itemExtras, err := responseObject(itemRaw, "type", "role", "call_id", "name", "arguments", "content", "summary", "encrypted_content", "signature")
		if err != nil {
			return nil, native.Usage{}, err
		}
		metadata.absorb(itemExtras)
		switch rawString(item["type"]) {
		case "reasoning":
			reasoning, encrypted, err := responsesReasoningText(item)
			if err != nil {
				return nil, native.Usage{}, err
			}
			signature := rawString(item["signature"])
			if encrypted != "" {
				if reasoning != "" {
					return nil, native.Usage{}, errors.New("reasoning item combines visible text with encrypted content")
				}
				blocks = append(blocks, map[string]any{"type": "redacted_thinking", "data": encrypted})
				continue
			}
			if reasoning == "" && signature == "" {
				continue
			}
			block := map[string]any{"type": "thinking", "thinking": reasoning}
			if signature != "" {
				block["signature"] = signature
			}
			blocks = append(blocks, block)
		case "message":
			if role := rawString(item["role"]); role != "" && role != "assistant" {
				return nil, native.Usage{}, errors.New("Responses response role must be assistant")
			}
			var parts []json.RawMessage
			if json.Unmarshal(item["content"], &parts) != nil {
				return nil, native.Usage{}, errors.New("Responses message content must be an array")
			}
			for _, partRaw := range parts {
				part, partExtras, err := responseObject(partRaw, "type", "text", "refusal", "annotations")
				if err != nil {
					return nil, native.Usage{}, err
				}
				metadata.absorb(partExtras)
				switch rawString(part["type"]) {
				case "output_text":
					if text := rawString(part["text"]); text != "" {
						textBlock := map[string]any{"type": "text", "text": text}
						// Annotations carry citations; Anthropic text blocks
						// host them under citations so the round-trip holds.
						if len(part["annotations"]) != 0 {
							var annotations []json.RawMessage
							if json.Unmarshal(part["annotations"], &annotations) != nil {
								return nil, native.Usage{}, errors.New("invalid output_text annotations")
							}
							var carried []any
							for _, entry := range annotations {
								carried = append(carried, json.RawMessage(entry))
							}
							textBlock["citations"] = carried
						}
						blocks = append(blocks, textBlock)
					}
				case "refusal":
					// A refusal is display text to the recipient, so it
					// converts as an ordinary text block.
					if refusal := rawString(part["refusal"]); refusal != "" {
						blocks = append(blocks, map[string]string{"type": "text", "text": refusal})
					}
				default:
					return nil, native.Usage{}, fmt.Errorf("Responses content part %q cannot be converted", rawString(part["type"]))
				}
			}
		case "function_call":
			input, err := chatToolArgsObject(rawString(item["arguments"]))
			if err != nil {
				return nil, native.Usage{}, fmt.Errorf("Responses function_call: %w", err)
			}
			callID := rawString(item["call_id"])
			if callID == "" || rawString(item["name"]) == "" {
				return nil, native.Usage{}, errors.New("Responses function_call requires a call_id and name")
			}
			sawTool = true
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": callID, "name": rawString(item["name"]), "input": input})
		default:
			return nil, native.Usage{}, fmt.Errorf("Responses output item %q cannot be converted", rawString(item["type"]))
		}
	}
	var status struct {
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &status) != nil {
		return nil, native.Usage{}, errors.New("invalid Responses response")
	}
	if status.Error != nil && status.Error.Message != "" {
		return nil, native.Usage{}, fmt.Errorf("upstream Responses request failed: %s", status.Error.Message)
	}
	stop := ""
	switch status.Status {
	case "completed":
		stop = "end_turn"
		if sawTool {
			stop = "tool_use"
		}
	case "incomplete":
		reason := ""
		if status.IncompleteDetails != nil {
			reason = status.IncompleteDetails.Reason
		}
		switch reason {
		case "max_output_tokens":
			stop = "max_tokens"
		case "content_filter":
			stop = "refusal"
		default:
			return nil, native.Usage{}, fmt.Errorf("Responses incomplete reason %q cannot be converted", reason)
		}
	default:
		return nil, native.Usage{}, fmt.Errorf("Responses status %q cannot be converted", status.Status)
	}
	usage := native.Usage{Complete: true}
	messageUsage := map[string]any{"input_tokens": 0, "output_tokens": 0}
	if nonNull(fields["usage"]) {
		details, err := responsesUsageDetails(fields["usage"])
		if err != nil {
			return nil, native.Usage{}, err
		}
		usage = details
		usage.Complete = true
		messageUsage = anthropicUsageEnvelope(usage)
	}
	result := map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": requestedModel,
		"content": blocks, "stop_reason": stop, "stop_sequence": nil, "usage": messageUsage,
	}
	if m := metadata.value(); m != nil {
		result["provider_metadata"] = m
	}
	converted, err := json.Marshal(result)
	return converted, usage, err
}

// responsesUsageDetails reads the Responses usage envelope shared by the sync
// and streaming Responses-to-Anthropic converters into the shared usage with
// its cache and reasoning details.
func responsesUsageDetails(raw json.RawMessage) (native.Usage, error) {
	usage, extra, err := rawObject(raw, "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details")
	if err != nil {
		return native.Usage{}, fmt.Errorf("invalid Responses usage: %w", err)
	}
	if len(extra) > 0 {
		logger.SysWarnf("Responses usage extras folded into provider_metadata: %s", sortExtrasNames(extra))
	}
	input, ok := decodeStreamNumber(usage["input_tokens"])
	if !ok || !nonNull(usage["input_tokens"]) {
		return native.Usage{}, errors.New("Responses usage requires input_tokens")
	}
	output, ok := decodeStreamNumber(usage["output_tokens"])
	if !ok {
		return native.Usage{}, errors.New("Responses usage requires output_tokens")
	}
	details := native.Usage{Input: input, Output: output, Seen: true}
	if nonNull(usage["input_tokens_details"]) {
		cachedFields, _, err := rawObject(usage["input_tokens_details"], "cached_tokens")
		if err != nil {
			return native.Usage{}, fmt.Errorf("invalid Responses usage details: %w", err)
		}
		cached, ok := decodeStreamNumber(cachedFields["cached_tokens"])
		if !ok {
			return native.Usage{}, errors.New("invalid Responses usage")
		}
		details.CacheRead = cached
	}
	if nonNull(usage["output_tokens_details"]) {
		reasoningFields, _, err := rawObject(usage["output_tokens_details"], "reasoning_tokens")
		if err != nil {
			return native.Usage{}, fmt.Errorf("invalid Responses usage details: %w", err)
		}
		reasoning, ok := decodeStreamNumber(reasoningFields["reasoning_tokens"])
		if !ok {
			return native.Usage{}, errors.New("invalid Responses usage")
		}
		details.Reasoning = reasoning
	}
	return details, nil
}
