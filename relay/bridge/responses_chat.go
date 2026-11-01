package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// responsesChat converts the Responses client wire onto Chat Completions
// upstreams. Responses is the richer wire, so the conversion is a projection:
// items and fields without a Chat representation are rejected before the
// upstream call. Visible reasoning history maps onto the Chat wire's
// reasoning_content slot; encrypted reasoning blobs have no Chat carrier and
// are rejected. It is the reverse of chatResponses and serves Responses
// clients on channels whose native wire is Chat.
type responsesChat struct{}

// Request converts the representable Responses subset. Streaming requests
// convert their body; the response stream uses the paired StreamConverter.
func (responsesChat) Request(body []byte, model string) ([]byte, error) {
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
	converted := map[string]any{"model": model}
	if stream {
		converted["stream"] = true
	}
	if limit > 0 {
		converted["max_tokens"] = limit
	}
	var chatMessages []any
	conversationStarted := false
	if len(fields["instructions"]) != 0 {
		var instructions string
		if json.Unmarshal(fields["instructions"], &instructions) != nil {
			return nil, errors.New("instructions must be a string")
		}
		chatMessages = append(chatMessages, map[string]any{"role": "system", "content": instructions})
	}
	chatMessages, conversationStarted, err = responsesInputItems(fields["input"], chatMessages, conversationStarted)
	if err != nil {
		return nil, err
	}
	converted["messages"] = chatMessages
	for _, key := range []string{"temperature", "top_p"} {
		if len(fields[key]) != 0 {
			converted[key] = json.RawMessage(fields[key])
		}
	}
	if len(fields["tools"]) != 0 {
		converted["tools"], err = responsesTools(fields["tools"])
		if err != nil {
			return nil, err
		}
	}
	if len(fields["tool_choice"]) != 0 {
		converted["tool_choice"], err = chatToolChoice(fields["tool_choice"])
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(converted)
}

// responsesInputItems converts the input array, or the plain-string shorthand,
// into Chat messages. System items must precede the conversation so the
// downstream Chat converters, which require the same ordering, accept them.
func responsesInputItems(raw json.RawMessage, chatMessages []any, conversationStarted bool) ([]any, bool, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return append(chatMessages, map[string]any{"role": "user", "content": text}), true, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return nil, false, errors.New("Responses input must be a string or a non-empty item array")
	}
	for _, itemRaw := range items {
		item, err := object(itemRaw, "type", "role", "content", "call_id", "name", "arguments", "output", "id", "status", "summary", "encrypted_content", "signature")
		if err != nil {
			return nil, false, err
		}
		switch rawString(item["type"]) {
		case "reasoning":
			// Visible reasoning history maps onto the assistant
			// reasoning_content slot. An encrypted blob is an opaque token of
			// the vendor that issued it and cannot cross to a Chat upstream.
			reasoning, encrypted, err := responsesReasoningText(item)
			if err != nil {
				return nil, false, err
			}
			if encrypted != "" {
				return nil, false, errors.New("encrypted reasoning cannot cross to a Chat upstream; use the Responses endpoint")
			}
			if reasoning != "" {
				chatMessages = append(chatMessages, map[string]any{"role": "assistant", "content": nil, "reasoning_content": reasoning})
				conversationStarted = true
			}
		case "message":
			message, role, err := responsesMessageItem(item)
			if err != nil {
				return nil, false, err
			}
			if role == "system" && conversationStarted {
				return nil, false, errors.New("system input items must precede conversation items")
			}
			chatMessages = append(chatMessages, message)
			if role != "system" {
				conversationStarted = true
			}
		case "function_call":
			callID, name := rawString(item["call_id"]), rawString(item["name"])
			arguments := rawString(item["arguments"])
			if callID == "" || name == "" || !json.Valid([]byte(arguments)) {
				return nil, false, errors.New("Responses function_call requires a call_id, name, and valid JSON arguments")
			}
			chatMessages = append(chatMessages, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{"id": callID, "type": "function", "function": map[string]string{"name": name, "arguments": arguments}},
			}})
			conversationStarted = true
		case "function_call_output":
			callID, output := rawString(item["call_id"]), rawString(item["output"])
			if callID == "" || output == "" {
				return nil, false, errors.New("Responses function_call_output requires a call_id and output")
			}
			chatMessages = append(chatMessages, map[string]any{"role": "tool", "tool_call_id": callID, "content": output})
			conversationStarted = true
		default:
			return nil, false, fmt.Errorf("Responses input item %q cannot be converted", rawString(item["type"]))
		}
	}
	return chatMessages, conversationStarted, nil
}

// responsesReasoningText extracts the visible reasoning text and the encrypted
// blob from a reasoning item. summary entries and reasoning_text content both
// carry visible text; they are joined in order.
func responsesReasoningText(item map[string]json.RawMessage) (string, string, error) {
	var visible []string
	var encrypted string
	if len(item["summary"]) != 0 {
		var entries []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(item["summary"], &entries) != nil {
			return "", "", errors.New("reasoning summary must be an array of summary entries")
		}
		for _, entry := range entries {
			visible = append(visible, entry.Text)
		}
	}
	if len(item["content"]) != 0 {
		var entries []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(item["content"], &entries) != nil {
			return "", "", errors.New("reasoning content must be an array of text entries")
		}
		for _, entry := range entries {
			visible = append(visible, entry.Text)
		}
	}
	if len(item["encrypted_content"]) != 0 {
		if json.Unmarshal(item["encrypted_content"], &encrypted) != nil || encrypted == "" {
			return "", "", errors.New("reasoning encrypted_content must be a string")
		}
	}
	return joinReasoning(visible), encrypted, nil
}

func joinReasoning(parts []string) string {
	result := ""
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i > 0 && result != "" {
			result += "\n"
		}
		result += part
	}
	return result
}

// responsesMessageItem converts one message item. User content carries input
// parts, assistant content output parts; a plain string is accepted for both.
func responsesMessageItem(item map[string]json.RawMessage) (map[string]any, string, error) {
	role := rawString(item["role"])
	switch role {
	case "user", "assistant", "system", "developer":
	default:
		return nil, "", fmt.Errorf("Responses message role %q cannot be converted", role)
	}
	var plain string
	if json.Unmarshal(item["content"], &plain) == nil {
		return map[string]any{"role": role, "content": plain}, role, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(item["content"], &parts) != nil || len(parts) == 0 {
		return nil, "", errors.New("Responses message content must be a string or a non-empty part array")
	}
	if role == "system" || role == "developer" {
		return nil, "", errors.New("system message content must be text")
	}
	if role == "user" {
		content, err := responsesUserParts(parts)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"role": role, "content": content}, role, nil
	}
	var text, refusal string
	for _, partRaw := range parts {
		part, err := object(partRaw, "type", "text", "refusal", "annotations")
		if err != nil {
			return nil, "", err
		}
		switch rawString(part["type"]) {
		case "output_text":
			if len(part["annotations"]) != 0 {
				var annotations []json.RawMessage
				if json.Unmarshal(part["annotations"], &annotations) != nil || len(annotations) > 0 {
					return nil, "", errors.New("output_text annotations have no Chat representation")
				}
			}
			var value string
			if json.Unmarshal(part["text"], &value) != nil {
				return nil, "", errors.New("output_text part requires text")
			}
			text += value
		case "refusal":
			if len(part) > 2 {
				return nil, "", errors.New("refusal part has fields Chat cannot represent")
			}
			var value string
			if json.Unmarshal(part["refusal"], &value) != nil {
				return nil, "", errors.New("refusal part requires refusal")
			}
			refusal += value
		default:
			return nil, "", fmt.Errorf("Responses assistant part %q cannot be converted", rawString(part["type"]))
		}
	}
	message := map[string]any{"role": role}
	if text != "" {
		message["content"] = text
	} else {
		message["content"] = nil
	}
	if refusal != "" {
		message["refusal"] = refusal
	}
	return message, role, nil
}

// responsesUserParts converts user content parts. Images pass as data or
// remote URLs; other part types have no Chat representation and fail.
func responsesUserParts(parts []json.RawMessage) ([]any, error) {
	var content []any
	for _, partRaw := range parts {
		part, err := object(partRaw, "type", "text", "image_url", "detail")
		if err != nil {
			return nil, err
		}
		switch rawString(part["type"]) {
		case "input_text":
			if len(part) != 2 {
				return nil, errors.New("input_text part has fields Chat cannot represent")
			}
			var value string
			if json.Unmarshal(part["text"], &value) != nil {
				return nil, errors.New("input_text part requires text")
			}
			content = append(content, map[string]string{"type": "text", "text": value})
		case "input_image":
			image := map[string]any{"type": "image_url"}
			var url string
			if json.Unmarshal(part["image_url"], &url) != nil || url == "" {
				return nil, errors.New("input_image part requires an image_url string")
			}
			inner := map[string]string{"url": url}
			if len(part["detail"]) != 0 {
				var detail string
				if json.Unmarshal(part["detail"], &detail) != nil {
					return nil, errors.New("input_image detail must be a string")
				}
				inner["detail"] = detail
			}
			image["image_url"] = inner
			content = append(content, image)
		default:
			return nil, fmt.Errorf("Responses user part %q cannot be converted", rawString(part["type"]))
		}
	}
	return content, nil
}

// responsesTools converts function tool definitions onto the Chat envelope.
func responsesTools(raw json.RawMessage) ([]any, error) {
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
		function := map[string]any{"name": name}
		if len(tool["description"]) != 0 {
			function["description"] = json.RawMessage(tool["description"])
		}
		if len(tool["parameters"]) != 0 {
			function["parameters"] = json.RawMessage(tool["parameters"])
		}
		if len(tool["strict"]) != 0 {
			function["strict"] = json.RawMessage(tool["strict"])
		}
		converted = append(converted, map[string]any{"type": "function", "function": function})
	}
	return converted, nil
}

// Response converts a non-streaming Chat Completions body into a Responses
// response object.
func (responsesChat) Response(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	return ChatToResponsesResponse(body, requestedModel)
}

// ChatToResponsesResponse converts a non-streaming Chat Completions body into
// a Responses response object. It is the inverse of responsesToChat and the
// response hop of the provider paths that present the Responses wire over
// chat-speaking channels.
func ChatToResponsesResponse(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	fields, extras, err := responseObject(body, "id", "object", "created", "model", "choices", "usage", "provider_metadata")
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata := newProviderMetadata(extras)
	var id string
	_ = json.Unmarshal(fields["id"], &id)
	var objectType string
	_ = json.Unmarshal(fields["object"], &objectType)
	var choices []json.RawMessage
	if id == "" || objectType != "chat.completion" || json.Unmarshal(fields["choices"], &choices) != nil || len(choices) != 1 {
		return nil, native.Usage{}, errors.New("Chat response requires an id, chat.completion object, and one choice")
	}
	choice, choiceExtras, err := responseObject(choices[0], "index", "message", "finish_reason")
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata.absorb(choiceExtras)
	var index int
	if json.Unmarshal(choice["index"], &index) != nil || index != 0 {
		return nil, native.Usage{}, errors.New("Chat response choice index must be zero")
	}
	var finish string
	_ = json.Unmarshal(choice["finish_reason"], &finish)
	status := "completed"
	var incompleteDetails any
	switch finish {
	case "stop", "tool_calls":
	case "length":
		status = "incomplete"
		incompleteDetails = map[string]string{"reason": "max_output_tokens"}
	case "content_filter":
		status = "incomplete"
		incompleteDetails = map[string]string{"reason": "content_filter"}
	default:
		return nil, native.Usage{}, fmt.Errorf("Chat finish_reason %q cannot be converted", finish)
	}
	message, messageExtras, err := responseObject(choice["message"], "role", "content", "refusal", "tool_calls", "reasoning_content")
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata.absorb(messageExtras)
	var role string
	_ = json.Unmarshal(message["role"], &role)
	if role != "assistant" {
		return nil, native.Usage{}, errors.New("Chat response message must be assistant")
	}
	output := []any{}
	// Visible upstream thinking becomes a reasoning item preceding the
	// assistant message, matching the Responses wire's item ordering.
	if reasoning := rawString(message["reasoning_content"]); reasoning != "" {
		output = append(output, map[string]any{
			"type": "reasoning", "id": newResponsesItemID("rsn_"), "summary": []any{map[string]string{"type": "summary_text", "text": reasoning}},
		})
	}
	if text, err := chatTextContent(message["content"]); err != nil {
		return nil, native.Usage{}, err
	} else if text != "" {
		output = append(output, map[string]any{
			"type": "message", "id": newResponsesItemID("msg_"), "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
		})
	}
	if refusal := rawString(message["refusal"]); refusal != "" {
		output = append(output, map[string]any{
			"type": "message", "id": newResponsesItemID("msg_"), "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "refusal", "refusal": refusal}},
		})
	}
	if len(message["tool_calls"]) != 0 {
		calls, err := chatToolCalls(message["tool_calls"])
		if err != nil {
			return nil, native.Usage{}, err
		}
		for _, call := range calls {
			if !json.Valid([]byte(call.Arguments)) {
				return nil, native.Usage{}, errors.New("function tool arguments must be valid JSON")
			}
			output = append(output, map[string]any{
				"type": "function_call", "id": newResponsesItemID("fc_"), "status": "completed",
				"call_id": call.ID, "name": call.Name, "arguments": call.Arguments,
			})
		}
	}
	usage := native.Usage{Complete: true}
	if nonNull(fields["usage"]) {
		details, err := chatUsageDetails(fields["usage"])
		if err != nil {
			return nil, native.Usage{}, err
		}
		usage = details
		usage.Complete = true
	}
	result := map[string]any{
		"id": id, "object": "response", "created_at": responseCreatedAt(fields["created"]),
		"status": status, "model": requestedModel, "output": output,
	}
	if incompleteDetails != nil {
		result["incomplete_details"] = incompleteDetails
	}
	if usage.Seen {
		result["usage"] = responsesUsageEnvelope(usage)
	}
	if m := metadata.value(); m != nil {
		result["provider_metadata"] = m
	}
	converted, err := json.Marshal(result)
	return converted, usage, err
}

// chatUsageDetails reads the Chat usage envelope shared by every chat-speaking
// provider, including the DeepSeek-style cache-hit fields, into the shared
// usage with its cache and reasoning details.
func chatUsageDetails(raw json.RawMessage) (native.Usage, error) {
	fields, _, err := responseObject(raw, "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens")
	if err != nil {
		return native.Usage{}, err
	}
	var source struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		PromptDetails    struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			ReasoningTokens int64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
		PromptCacheHitTokens  int64 `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int64 `json:"prompt_cache_miss_tokens"`
	}
	if json.Unmarshal(raw, &source) != nil || len(fields["prompt_tokens"]) == 0 || len(fields["completion_tokens"]) == 0 ||
		source.PromptDetails.CachedTokens < 0 || source.PromptCacheHitTokens < 0 || source.PromptCacheMissTokens < 0 ||
		source.PromptTokens < 0 || source.CompletionTokens < 0 {
		return native.Usage{}, errors.New("invalid Chat usage")
	}
	cached := max(source.PromptDetails.CachedTokens, source.PromptCacheHitTokens)
	if source.PromptTokens < cached {
		return native.Usage{}, errors.New("invalid Chat usage")
	}
	return native.Usage{
		Input: source.PromptTokens, Output: source.CompletionTokens,
		CacheRead: cached, Reasoning: source.CompletionDetails.ReasoningTokens,
		Seen: true,
	}, nil
}

// responseCreatedAt passes the Chat created timestamp through, defaulting to
// now when the upstream omitted it.
func responseCreatedAt(raw json.RawMessage) int64 {
	var created int64
	if json.Unmarshal(raw, &created) != nil || created <= 0 {
		return streamTimestamp()
	}
	return created
}

// newResponsesItemID mints an output item identifier with the Responses
// convention for the item kind.
func newResponsesItemID(prefix string) string {
	id, err := newStreamID(prefix)
	if err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return id
}
