package bridge

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// responsesAnthropic converts the Responses client wire onto Anthropic
// Messages upstreams, and Anthropic message responses back onto the Responses
// wire. Responses is the richer wire, so the request conversion is a
// projection: items and fields without a Messages representation are rejected
// before the upstream call. Thinking content round-trips in both directions:
// Claude thinking blocks map onto reasoning items with their signature, and
// redacted thinking maps onto the reasoning item's encrypted_content slot so
// the opaque blob returns to the vendor that issued it.
type responsesAnthropic struct{}

// Request converts the representable Responses subset onto the Messages wire.
func (responsesAnthropic) Request(body []byte, model string) ([]byte, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	fields, err := object(body, "model", "input", "instructions", "stream", "max_output_tokens",
		"temperature", "top_p", "tools", "tool_choice")
	if err != nil {
		return nil, err
	}
	var stream bool
	if err := decode(fields["stream"], &stream); err != nil {
		return nil, errors.New("stream must be boolean")
	}
	var limit int64
	if err := decode(fields["max_output_tokens"], &limit); err != nil || limit < 0 {
		return nil, errors.New("max_output_tokens must not be negative")
	}
	if limit == 0 {
		limit = 1024
	}
	request := map[string]any{"model": model, "max_tokens": limit}
	if stream {
		request["stream"] = true
	}
	var system []string
	var blocks []any
	var pendingThinking []any
	conversationStarted := false
	appendSystem := func(text string) error {
		if conversationStarted {
			return errors.New("system instructions must precede conversation items")
		}
		system = append(system, text)
		return nil
	}
	// attachThinking folds pending reasoning blocks into the next assistant
	// message content, which is where Messages expects thinking to live.
	attachThinking := func(content []any) []any {
		if len(pendingThinking) == 0 {
			return content
		}
		merged := append([]any{}, pendingThinking...)
		pendingThinking = nil
		return append(merged, content...)
	}
	if len(fields["instructions"]) != 0 {
		var instructions string
		if json.Unmarshal(fields["instructions"], &instructions) != nil {
			return nil, errors.New("instructions must be a string")
		}
		if err := appendSystem(instructions); err != nil {
			return nil, err
		}
	}
	items, err := responsesInputArray(fields["input"])
	if err != nil {
		return nil, err
	}
	for _, itemRaw := range items {
		item, err := object(itemRaw, "type", "role", "content", "call_id", "name", "arguments", "output", "id", "status", "summary", "encrypted_content", "signature")
		if err != nil {
			return nil, err
		}
		switch rawString(item["type"]) {
		case "reasoning":
			reasoning, encrypted, err := responsesReasoningText(item)
			if err != nil {
				return nil, err
			}
			signature := rawString(item["signature"])
			if encrypted != "" {
				if reasoning != "" {
					return nil, errors.New("reasoning item combines visible text with encrypted content")
				}
				pendingThinking = append(pendingThinking, map[string]any{"type": "redacted_thinking", "data": encrypted})
				continue
			}
			if reasoning == "" && signature == "" {
				continue
			}
			block := map[string]any{"type": "thinking", "thinking": reasoning}
			if signature != "" {
				block["signature"] = signature
			}
			pendingThinking = append(pendingThinking, block)
		case "message":
			role := rawString(item["role"])
			switch role {
			case "system", "developer":
				var text string
				if json.Unmarshal(item["content"], &text) != nil {
					return nil, errors.New("system message content must be text")
				}
				if err := appendSystem(text); err != nil {
					return nil, err
				}
			case "user":
				if len(pendingThinking) > 0 {
					return nil, errors.New("reasoning items must precede an assistant item")
				}
				userBlocks, err := responsesUserBlocks(item["content"])
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, map[string]any{"role": "user", "content": userBlocks})
				conversationStarted = true
			case "assistant":
				assistantBlocks, err := responsesAssistantBlocks(item["content"])
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, map[string]any{"role": "assistant", "content": attachThinking(assistantBlocks)})
				conversationStarted = true
			default:
				return nil, fmt.Errorf("Responses message role %q cannot be converted", role)
			}
		case "function_call":
			callID, name := rawString(item["call_id"]), rawString(item["name"])
			input, err := chatToolArgsObject(rawString(item["arguments"]))
			if err != nil {
				return nil, fmt.Errorf("Responses function_call: %w", err)
			}
			if callID == "" || name == "" {
				return nil, errors.New("Responses function_call requires a call_id and name")
			}
			blocks = append(blocks, map[string]any{"role": "assistant", "content": attachThinking([]any{
				map[string]any{"type": "tool_use", "id": callID, "name": name, "input": input},
			})})
			conversationStarted = true
		case "function_call_output":
			if len(pendingThinking) > 0 {
				return nil, errors.New("reasoning items must precede an assistant item")
			}
			callID, output := rawString(item["call_id"]), rawString(item["output"])
			if callID == "" || output == "" {
				return nil, errors.New("Responses function_call_output requires a call_id and output")
			}
			blocks = append(blocks, map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": callID, "content": output},
			}})
			conversationStarted = true
		default:
			return nil, fmt.Errorf("Responses input item %q cannot be converted", rawString(item["type"]))
		}
	}
	if len(pendingThinking) > 0 {
		return nil, errors.New("reasoning items must precede an assistant item")
	}
	if len(blocks) == 0 {
		return nil, errors.New("Responses input produced no convertible messages")
	}
	request["messages"] = blocks
	if len(system) > 0 {
		request["system"] = joinSystem(system)
	}
	for _, key := range []string{"temperature", "top_p"} {
		if len(fields[key]) != 0 {
			request[key] = json.RawMessage(fields[key])
		}
	}
	if len(fields["tools"]) != 0 {
		converted, err := responsesToolsToAnthropic(fields["tools"])
		if err != nil {
			return nil, err
		}
		request["tools"] = converted
	}
	if len(fields["tool_choice"]) != 0 {
		choice, err := responsesToolChoiceToAnthropic(fields["tool_choice"])
		if err != nil {
			return nil, err
		}
		request["tool_choice"] = choice
	}
	return json.Marshal(request)
}

// responsesInputArray reads the input field: a plain string is the shorthand
// for one user message.
func responsesInputArray(raw json.RawMessage) ([]json.RawMessage, error) {
	var text string
	if len(raw) != 0 && json.Unmarshal(raw, &text) == nil {
		encoded, err := json.Marshal(map[string]any{"type": "message", "role": "user", "content": text})
		if err != nil {
			return nil, err
		}
		return []json.RawMessage{encoded}, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return nil, errors.New("Responses input must be a string or a non-empty item array")
	}
	return items, nil
}

func responsesUserBlocks(raw json.RawMessage) ([]any, error) {
	var plain string
	if len(raw) != 0 && json.Unmarshal(raw, &plain) == nil {
		if plain == "" {
			return nil, errors.New("Responses message content is empty")
		}
		return []any{map[string]string{"type": "text", "text": plain}}, nil
	}
	parts, err := responsesContentParts(raw, "input_text", "input_image")
	if err != nil {
		return nil, err
	}
	var blocks []any
	for _, part := range parts {
		switch rawString(part["type"]) {
		case "input_text":
			if len(part) != 2 {
				return nil, errors.New("input_text part has fields Messages cannot represent")
			}
			blocks = append(blocks, map[string]string{"type": "text", "text": rawString(part["text"])})
		case "input_image":
			image, err := responsesImageToAnthropic(part["image_url"])
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, image)
		}
	}
	if len(blocks) == 0 {
		return nil, errors.New("Responses message content is empty")
	}
	return blocks, nil
}

// responsesAssistantBlocks converts assistant message content onto Messages
// blocks. A refusal part is display text to the recipient, so it converts as
// an ordinary text block.
func responsesAssistantBlocks(raw json.RawMessage) ([]any, error) {
	var plain string
	if len(raw) != 0 && json.Unmarshal(raw, &plain) == nil {
		if plain == "" {
			return nil, errors.New("Responses message content is empty")
		}
		return []any{map[string]string{"type": "text", "text": plain}}, nil
	}
	parts, err := responsesContentParts(raw, "output_text", "refusal")
	if err != nil {
		return nil, err
	}
	var blocks []any
	for _, part := range parts {
		switch rawString(part["type"]) {
		case "output_text":
			if len(part) != 2 {
				return nil, errors.New("output_text part has fields Messages cannot represent")
			}
			blocks = append(blocks, map[string]string{"type": "text", "text": rawString(part["text"])})
		case "refusal":
			if len(part) != 2 {
				return nil, errors.New("refusal part has fields Messages cannot represent")
			}
			blocks = append(blocks, map[string]string{"type": "text", "text": rawString(part["refusal"])})
		}
	}
	if len(blocks) == 0 {
		return nil, errors.New("Responses message content is empty")
	}
	return blocks, nil
}

// responsesContentParts parses a content part array restricted to the given
// part kinds; the allowed keys are the union of both part shapes.
func responsesContentParts(raw json.RawMessage, kinds ...string) ([]map[string]json.RawMessage, error) {
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return nil, errors.New("Responses message content must be a string or a non-empty part array")
	}
	permitted := map[string]bool{}
	for _, kind := range kinds {
		permitted[kind] = true
	}
	var result []map[string]json.RawMessage
	for _, partRaw := range parts {
		part, err := object(partRaw, "type", "text", "refusal", "image_url", "annotations")
		if err != nil {
			return nil, err
		}
		if !permitted[rawString(part["type"])] {
			return nil, fmt.Errorf("Responses content part %q cannot be converted", rawString(part["type"]))
		}
		if len(part["annotations"]) != 0 {
			var annotations []json.RawMessage
			if json.Unmarshal(part["annotations"], &annotations) != nil || len(annotations) > 0 {
				return nil, errors.New("output_text annotations have no Messages representation")
			}
		}
		result = append(result, part)
	}
	return result, nil
}

// responsesImageToAnthropic converts an input_image part onto a Messages image
// block. Base64 data URLs convert to source objects; remote URLs pass through
// for the upstream to fetch.
func responsesImageToAnthropic(raw json.RawMessage) (map[string]any, error) {
	var address string
	if json.Unmarshal(raw, &address) != nil || address == "" {
		return nil, errors.New("input_image part requires an image_url string")
	}
	if len(address) > 5 && address[:5] == "data:" {
		mediaType, data, err := parseImageDataURL(address)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": mediaType, "data": data}}, nil
	}
	if err := validateImageURL(address); err != nil {
		return nil, err
	}
	return map[string]any{"type": "image", "source": map[string]string{"type": "url", "url": address}}, nil
}

// responsesToolsToAnthropic converts Responses function tools onto Messages
// client tools. strict has no Messages equivalent and is rejected.
func responsesToolsToAnthropic(raw json.RawMessage) ([]any, error) {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return nil, errors.New("tools must be an array")
	}
	var converted []any
	for _, toolRaw := range tools {
		tool, err := object(toolRaw, "type", "name", "description", "parameters", "strict")
		if err != nil {
			return nil, err
		}
		if rawString(tool["type"]) != "function" {
			return nil, errors.New("only function tools can be converted")
		}
		var name string
		_ = json.Unmarshal(tool["name"], &name)
		if name == "" {
			return nil, errors.New("function tool name is required")
		}
		if nonNull(tool["strict"]) {
			return nil, errors.New("function tool strict mode cannot be converted")
		}
		var schema map[string]any
		if json.Unmarshal(tool["parameters"], &schema) != nil || schema == nil {
			return nil, errors.New("function tool parameters must be a JSON object")
		}
		client := map[string]any{"name": name, "input_schema": json.RawMessage(tool["parameters"])}
		if len(tool["description"]) != 0 {
			client["description"] = json.RawMessage(tool["description"])
		}
		converted = append(converted, client)
	}
	return converted, nil
}

// responsesToolChoiceToAnthropic converts the Responses tool_choice values.
func responsesToolChoiceToAnthropic(raw json.RawMessage) (map[string]string, error) {
	var choice string
	if json.Unmarshal(raw, &choice) == nil {
		switch choice {
		case "auto", "none":
			return map[string]string{"type": choice}, nil
		case "required":
			return map[string]string{"type": "any"}, nil
		default:
			return nil, fmt.Errorf("tool_choice %q cannot be converted", choice)
		}
	}
	choiceObject, err := object(raw, "type", "name")
	if err != nil {
		return nil, err
	}
	if rawString(choiceObject["type"]) != "function" {
		return nil, errors.New("only named function tool_choice can be converted")
	}
	var name string
	_ = json.Unmarshal(choiceObject["name"], &name)
	if name == "" {
		return nil, errors.New("named function tool_choice requires a name")
	}
	return map[string]string{"type": "tool", "name": name}, nil
}

// Response converts a non-streaming Messages response onto the Responses wire.
// It shares its parsing with the stream converter's terminal state.
func (responsesAnthropic) Response(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	fields, extras, err := responseObject(body, "id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage")
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata := newProviderMetadata(extras)
	if matched := rawString(fields["stop_sequence"]); matched != "" {
		metadata.set("matched_stop_sequence", matched)
	}
	var rawBlocks []json.RawMessage
	if json.Unmarshal(fields["content"], &rawBlocks) != nil {
		return nil, native.Usage{}, errors.New("Anthropic content must be an array")
	}
	var id string
	_ = json.Unmarshal(fields["id"], &id)
	if id == "" {
		id = newResponsesItemID("resp_")
	}
	if role := rawString(fields["role"]); role != "" && role != "assistant" {
		return nil, native.Usage{}, errors.New("Anthropic response role must be assistant")
	}
	if rawString(fields["type"]) != "message" {
		return nil, native.Usage{}, errors.New("invalid Anthropic message response")
	}
	output := []any{}
	sawTool := false
	for _, raw := range rawBlocks {
		output, sawTool, err = appendAnthropicBlockAsItem(output, sawTool, raw, metadata)
		if err != nil {
			return nil, native.Usage{}, err
		}
	}
	status := "completed"
	var incompleteDetails any
	switch rawString(fields["stop_reason"]) {
	case "end_turn", "stop_sequence", "tool_use", "refusal":
	case "max_tokens":
		status = "incomplete"
		incompleteDetails = map[string]string{"reason": "max_output_tokens"}
	default:
		return nil, native.Usage{}, fmt.Errorf("Anthropic stop reason %q cannot be converted", rawString(fields["stop_reason"]))
	}
	usage := native.Usage{Complete: true}
	result := map[string]any{
		"id": id, "object": "response", "created_at": streamTimestamp(),
		"status": status, "model": requestedModel, "output": output,
	}
	if incompleteDetails != nil {
		result["incomplete_details"] = incompleteDetails
	}
	if nonNull(fields["usage"]) {
		details, err := anthropicUsageDetails(fields["usage"])
		if err != nil {
			return nil, native.Usage{}, err
		}
		usage = details
		usage.Complete = true
		result["usage"] = responsesUsageEnvelope(usage)
	}
	if m := metadata.value(); m != nil {
		result["provider_metadata"] = m
	}
	converted, err := json.Marshal(result)
	return converted, usage, err
}

// appendAnthropicBlockAsItem converts one Anthropic content block into a
// Responses output item. Thinking blocks map onto reasoning items carrying
// their signature; redacted thinking rides the encrypted_content slot so the
// opaque blob round-trips to the vendor that issued it; citations map onto
// output_text annotations carried raw. Block-level extras the item vocabulary
// cannot host land in provider metadata. Shared with the streaming converter's
// block state machine.
func appendAnthropicBlockAsItem(output []any, sawTool bool, raw json.RawMessage, metadata *providerMetadata) ([]any, bool, error) {
	block, extras, err := responseObject(raw, "type", "text", "id", "name", "input", "thinking", "signature", "data", "citations")
	if err != nil {
		return output, sawTool, err
	}
	metadata.absorb(extras)
	switch rawString(block["type"]) {
	case "text":
		if len(block) != 2 && len(block["citations"]) == 0 {
			return output, sawTool, errors.New("invalid Anthropic text block")
		}
		if text := rawString(block["text"]); text != "" {
			part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
			if len(block["citations"]) != 0 {
				var citations []json.RawMessage
				if json.Unmarshal(block["citations"], &citations) != nil {
					return output, sawTool, errors.New("Anthropic citations must be an array")
				}
				var carried []any
				for _, citation := range citations {
					carried = append(carried, json.RawMessage(citation))
				}
				part["annotations"] = carried
			}
			output = append(output, map[string]any{
				"type": "message", "id": newResponsesItemID("msg_"), "status": "completed", "role": "assistant",
				"content": []any{part},
			})
		}
	case "thinking":
		text := rawString(block["thinking"])
		if text == "" {
			return output, sawTool, errors.New("Anthropic thinking block requires text")
		}
		item := map[string]any{
			"type": "reasoning", "id": newResponsesItemID("rsn_"),
			"summary": []any{map[string]string{"type": "summary_text", "text": text}},
		}
		if signature := rawString(block["signature"]); signature != "" {
			item["signature"] = signature
		}
		output = append(output, item)
	case "redacted_thinking":
		data := rawString(block["data"])
		if data == "" {
			return output, sawTool, errors.New("Anthropic redacted thinking requires data")
		}
		output = append(output, map[string]any{
			"type": "reasoning", "id": newResponsesItemID("rsn_"), "encrypted_content": data,
		})
	case "tool_use":
		if len(block) != 4 {
			return output, sawTool, errors.New("invalid Anthropic tool_use block")
		}
		var input map[string]any
		if json.Unmarshal(block["input"], &input) != nil || input == nil {
			return output, sawTool, errors.New("Anthropic tool_use input must be an object")
		}
		arguments, err := json.Marshal(input)
		if err != nil {
			return output, sawTool, err
		}
		callID := rawString(block["id"])
		if callID == "" || rawString(block["name"]) == "" {
			return output, sawTool, errors.New("Anthropic tool_use requires an id and name")
		}
		sawTool = true
		output = append(output, map[string]any{
			"type": "function_call", "id": newResponsesItemID("fc_"), "status": "completed",
			"call_id": callID, "name": rawString(block["name"]), "arguments": string(arguments),
		})
	default:
		return output, sawTool, fmt.Errorf("Anthropic content block %q cannot be converted", rawString(block["type"]))
	}
	return output, sawTool, nil
}

// anthropicUsageDetails reads the Messages usage envelope shared by the sync
// and streaming Anthropic-to-Responses converters into the shared usage with
// its cache breakdown. Input keeps including the cache tokens, matching how
// the gateway bills the request.
func anthropicUsageDetails(raw json.RawMessage) (native.Usage, error) {
	usage, extra, err := rawObject(raw, "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")
	if err != nil {
		return native.Usage{}, fmt.Errorf("invalid Anthropic usage: %w", err)
	}
	if len(extra) > 0 {
		logger.SysWarnf("Anthropic usage extras folded into provider_metadata: %s", sortExtrasNames(extra))
	}
	input, ok := decodeStreamNumber(usage["input_tokens"])
	if !ok || !nonNull(usage["input_tokens"]) {
		return native.Usage{}, errors.New("Anthropic usage requires input_tokens")
	}
	output, ok := decodeStreamNumber(usage["output_tokens"])
	if !ok {
		return native.Usage{}, errors.New("Anthropic usage requires output_tokens")
	}
	cacheCreation, ok := decodeStreamNumber(usage["cache_creation_input_tokens"])
	if !ok {
		return native.Usage{}, errors.New("invalid Anthropic usage")
	}
	cacheRead, ok := decodeStreamNumber(usage["cache_read_input_tokens"])
	if !ok {
		return native.Usage{}, errors.New("invalid Anthropic usage")
	}
	return native.Usage{
		Input: input + cacheCreation + cacheRead, Output: output,
		CacheCreation: cacheCreation, CacheRead: cacheRead, Seen: true,
	}, nil
}
