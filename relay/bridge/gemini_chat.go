package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// GeminiToChatRequest converts the GenerateContent request subset that Chat
// Completions can represent: text, inline base64 media, function tools, calls
// and results. Thought parts carry no upstream-visible content and are
// skipped, so replayed reasoning history does not break the request.
// Unrepresentable content fails before the upstream call. The stream intent
// is carried by the URL action, so the caller restores it on the converted
// body via EnsureChatStreamOptions.
func GeminiToChatRequest(body []byte, model string) ([]byte, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	fields, err := object(body, "contents", "systemInstruction", "generationConfig", "tools", "toolConfig")
	if err != nil {
		return nil, err
	}
	request := map[string]any{"model": model}
	if nonNull(fields["generationConfig"]) {
		limits, err := geminiGenerationConfig(fields["generationConfig"])
		if err != nil {
			return nil, err
		}
		for key, value := range limits {
			request[key] = value
		}
	}
	if len(fields["tools"]) != 0 {
		tools, err := geminiToolsToChat(fields["tools"])
		if err != nil {
			return nil, err
		}
		request["tools"] = tools
	}
	if len(fields["toolConfig"]) != 0 {
		choice, err := geminiToolConfigToChat(fields["toolConfig"])
		if err != nil {
			return nil, err
		}
		request["tool_choice"] = choice
	}
	var chatMessages []any
	if len(fields["systemInstruction"]) != 0 {
		text, err := geminiSystemInstruction(fields["systemInstruction"])
		if err != nil {
			return nil, err
		}
		if text != "" {
			chatMessages = append(chatMessages, map[string]any{"role": "system", "content": text})
		}
	}
	var contents []json.RawMessage
	if len(fields["contents"]) != 0 {
		if json.Unmarshal(fields["contents"], &contents) != nil {
			return nil, errors.New("contents must be an array")
		}
	}
	toolIDs := map[string]string{}
	callCounter := 0
	for _, raw := range contents {
		content, err := object(raw, "role", "parts")
		if err != nil {
			return nil, fmt.Errorf("invalid content entry: %w", err)
		}
		role := rawString(content["role"])
		if role == "" {
			// Single-turn GenerateContent requests commonly omit the role;
			// GenerateContent treats them as user input.
			role = "user"
		}
		if role != "user" && role != "model" {
			return nil, errors.New("Gemini content role must be user or model")
		}
		var rawParts []json.RawMessage
		if json.Unmarshal(content["parts"], &rawParts) != nil {
			return nil, errors.New("Gemini content parts must be an array")
		}
		if role == "user" {
			messages, err := geminiUserPartsToChat(rawParts, toolIDs)
			if err != nil {
				return nil, err
			}
			chatMessages = append(chatMessages, messages...)
			continue
		}
		var text string
		var toolCalls []any
		for _, rawPart := range rawParts {
			part, err := parseGeminiRequestPart(rawPart)
			if err != nil {
				return nil, err
			}
			switch part.kind {
			case geminiRequestPartText:
				text += part.text
			case geminiRequestPartFunctionCall:
				callCounter++
				callID := part.callID
				if callID == "" {
					callID = "call_" + strconv.Itoa(callCounter)
				}
				if existing, ok := toolIDs[part.name]; ok && existing != callID {
					return nil, errors.New("Gemini function call name maps to conflicting ids")
				}
				toolIDs[part.name] = callID
				toolCalls = append(toolCalls, map[string]any{"id": callID, "type": "function", "function": map[string]string{"name": part.name, "arguments": string(part.args)}})
			}
		}
		if text == "" && len(toolCalls) == 0 {
			// Thought-only model turns carry no content Chat can send.
			continue
		}
		message := map[string]any{"role": "assistant", "content": nil}
		if text != "" {
			message["content"] = text
		}
		if len(toolCalls) > 0 {
			message["tool_calls"] = toolCalls
		}
		chatMessages = append(chatMessages, message)
	}
	if len(chatMessages) == 0 {
		return nil, errors.New("Gemini request requires conversation contents")
	}
	request["messages"] = chatMessages
	return json.Marshal(request)
}

// geminiGenerationConfig maps the sampling limits Chat Completions shares
// with GenerateContent. generationConfig fields without a Chat counterpart
// are dropped by the whitelist.
func geminiGenerationConfig(raw json.RawMessage) (map[string]any, error) {
	config, err := object(raw, "maxOutputTokens", "temperature", "topP", "stopSequences")
	if err != nil {
		return nil, fmt.Errorf("generationConfig: %w", err)
	}
	limits := map[string]any{}
	var limit int64
	if err := json.Unmarshal(config["maxOutputTokens"], &limit); err != nil || limit < 0 {
		return nil, errors.New("generationConfig maxOutputTokens must not be negative")
	}
	if limit > 0 {
		limits["max_tokens"] = limit
	}
	for _, pair := range [][2]string{{"temperature", "temperature"}, {"topP", "top_p"}} {
		if len(config[pair[0]]) != 0 {
			var value float64
			if json.Unmarshal(config[pair[0]], &value) != nil {
				return nil, fmt.Errorf("generationConfig %s must be a number", pair[0])
			}
			limits[pair[1]] = value
		}
	}
	if len(config["stopSequences"]) != 0 {
		var stops []string
		if json.Unmarshal(config["stopSequences"], &stops) != nil {
			return nil, errors.New("generationConfig stopSequences must be an array of strings")
		}
		limits["stop"] = stops
	}
	return limits, nil
}

// geminiRequestPart is the converted view of one request content part.
type geminiRequestPart struct {
	kind     geminiRequestPartKind
	text     string
	mimeType string
	data     string
	name     string
	args     json.RawMessage
	callID   string
	response json.RawMessage
	resultID string
}

type geminiRequestPartKind int

const (
	geminiRequestPartText geminiRequestPartKind = iota
	geminiRequestPartInline
	geminiRequestPartFunctionCall
	geminiRequestPartFunctionResponse
	geminiRequestPartThought
)

// parseGeminiRequestPart parses one request content part. Thought parts are
// recognized so history replay skips them; a part with no representable
// content fails instead of being dropped as an empty entry.
func parseGeminiRequestPart(raw json.RawMessage) (geminiRequestPart, error) {
	fields, err := object(raw, "text", "thought", "thoughtSignature", "inlineData", "functionCall", "functionResponse")
	if err != nil {
		return geminiRequestPart{}, err
	}
	if len(fields["functionCall"]) != 0 {
		if len(fields["text"]) != 0 || len(fields["inlineData"]) != 0 || len(fields["functionResponse"]) != 0 {
			return geminiRequestPart{}, errors.New("Gemini part combines a function call with other content")
		}
		call, err := object(fields["functionCall"], "name", "args", "id")
		if err != nil {
			return geminiRequestPart{}, err
		}
		name := rawString(call["name"])
		if name == "" {
			return geminiRequestPart{}, errors.New("Gemini function call requires a name")
		}
		args := json.RawMessage("{}")
		if nonNull(call["args"]) {
			var input map[string]any
			if json.Unmarshal(call["args"], &input) != nil || input == nil {
				return geminiRequestPart{}, errors.New("Gemini function call args must be an object")
			}
			args = call["args"]
		}
		return geminiRequestPart{kind: geminiRequestPartFunctionCall, name: name, args: args, callID: rawString(call["id"])}, nil
	}
	if len(fields["functionResponse"]) != 0 {
		if len(fields["text"]) != 0 || len(fields["inlineData"]) != 0 {
			return geminiRequestPart{}, errors.New("Gemini part combines a function response with other content")
		}
		response, err := object(fields["functionResponse"], "name", "id", "response")
		if err != nil {
			return geminiRequestPart{}, err
		}
		if !nonNull(response["response"]) {
			return geminiRequestPart{}, errors.New("Gemini function response requires a response object")
		}
		return geminiRequestPart{kind: geminiRequestPartFunctionResponse, name: rawString(response["name"]), response: response["response"], resultID: rawString(response["id"])}, nil
	}
	if len(fields["inlineData"]) != 0 {
		media, err := object(fields["inlineData"], "mimeType", "data")
		if err != nil {
			return geminiRequestPart{}, err
		}
		mimeType := rawString(media["mimeType"])
		data := rawString(media["data"])
		if mimeType == "" || data == "" {
			return geminiRequestPart{}, errors.New("Gemini inline media requires a mimeType and base64 data")
		}
		return geminiRequestPart{kind: geminiRequestPartInline, mimeType: mimeType, data: data}, nil
	}
	var thought bool
	if len(fields["thought"]) != 0 {
		if json.Unmarshal(fields["thought"], &thought) != nil {
			return geminiRequestPart{}, errors.New("Gemini thought flag must be boolean")
		}
	}
	if thought || rawString(fields["text"]) == "" {
		// Thought parts and bare thought signatures replay no conversation
		// content; Chat has no input slot for them, so they are skipped.
		if thought || rawString(fields["thoughtSignature"]) != "" {
			return geminiRequestPart{kind: geminiRequestPartThought}, nil
		}
		return geminiRequestPart{}, errors.New("Gemini part cannot be represented")
	}
	return geminiRequestPart{kind: geminiRequestPartText, text: rawString(fields["text"])}, nil
}

// geminiUserPartsToChat converts one user turn. Text and inline media merge
// into a single user message; function responses become tool messages that
// follow it, matching the Chat wire order.
func geminiUserPartsToChat(rawParts []json.RawMessage, toolIDs map[string]string) ([]any, error) {
	var messages []any
	var parts []any
	for _, rawPart := range rawParts {
		part, err := parseGeminiRequestPart(rawPart)
		if err != nil {
			return nil, err
		}
		switch part.kind {
		case geminiRequestPartText:
			parts = append(parts, map[string]string{"type": "text", "text": part.text})
		case geminiRequestPartInline:
			media, err := geminiInlineToChat(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, media)
		case geminiRequestPartFunctionResponse:
			id := part.resultID
			if id == "" {
				id = toolIDs[part.name]
			}
			if part.name == "" || id == "" {
				return nil, errors.New("Gemini function response references an unknown function call")
			}
			if len(parts) > 0 {
				messages = append(messages, map[string]any{"role": "user", "content": parts})
				parts = nil
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": string(part.response)})
		}
	}
	if len(parts) > 0 {
		if text, ok := parts[0].(map[string]string); ok && len(parts) == 1 {
			messages = append(messages, map[string]any{"role": "user", "content": text["text"]})
		} else {
			messages = append(messages, map[string]any{"role": "user", "content": parts})
		}
	}
	return messages, nil
}

// geminiInlineToChat converts inline base64 media into its Chat content part.
func geminiInlineToChat(part geminiRequestPart) (map[string]any, error) {
	if len(part.mimeType) > 6 && part.mimeType[:6] == "image/" {
		return map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + part.mimeType + ";base64," + part.data}}, nil
	}
	if len(part.mimeType) > 6 && part.mimeType[:6] == "audio/" {
		return map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": part.data, "format": part.mimeType[6:]}}, nil
	}
	return nil, fmt.Errorf("Gemini inline media type %q cannot be converted", part.mimeType)
}

// geminiSystemInstruction reads a system instruction given as a string or a
// parts object, joining the text parts.
func geminiSystemInstruction(raw json.RawMessage) (string, error) {
	if text, ok := chatContentString(raw); ok {
		return text, nil
	}
	instruction, err := object(raw, "role", "parts")
	if err != nil {
		return "", fmt.Errorf("systemInstruction: %w", err)
	}
	var rawParts []json.RawMessage
	if json.Unmarshal(instruction["parts"], &rawParts) != nil {
		return "", errors.New("systemInstruction parts must be an array")
	}
	var text string
	for _, rawPart := range rawParts {
		part, err := object(rawPart, "text", "thought")
		if err != nil {
			return "", fmt.Errorf("systemInstruction part: %w", err)
		}
		var thought bool
		_ = json.Unmarshal(part["thought"], &thought)
		if thought {
			continue
		}
		text += rawString(part["text"])
	}
	return text, nil
}

// geminiToolsToChat converts Gemini function declarations into Chat tools.
// Tool entries without declarations (built-in grounding, code execution) have
// no Chat representation and fail.
func geminiToolsToChat(raw json.RawMessage) ([]any, error) {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return nil, errors.New("tools must be an array")
	}
	var converted []any
	for _, item := range tools {
		tool, err := object(item, "functionDeclarations")
		if err != nil {
			return nil, err
		}
		var declarations []json.RawMessage
		if json.Unmarshal(tool["functionDeclarations"], &declarations) != nil {
			return nil, errors.New("only function tools can be converted")
		}
		for _, declarationRaw := range declarations {
			declaration, err := object(declarationRaw, "name", "description", "parameters")
			if err != nil {
				return nil, err
			}
			name := rawString(declaration["name"])
			if name == "" {
				return nil, errors.New("function tool name is required")
			}
			function := map[string]any{"name": name}
			if len(declaration["description"]) != 0 {
				function["description"] = json.RawMessage(declaration["description"])
			}
			if nonNull(declaration["parameters"]) {
				var schema map[string]any
				if json.Unmarshal(declaration["parameters"], &schema) != nil || schema == nil {
					return nil, errors.New("function tool parameters must be a JSON object")
				}
				function["parameters"] = json.RawMessage(declaration["parameters"])
			}
			converted = append(converted, map[string]any{"type": "function", "function": function})
		}
	}
	if len(converted) == 0 {
		return nil, errors.New("tools must not be empty")
	}
	return converted, nil
}

// geminiToolConfigToChat converts the Gemini function calling config into the
// Chat tool_choice values.
func geminiToolConfigToChat(raw json.RawMessage) (any, error) {
	config, err := object(raw, "functionCallingConfig")
	if err != nil {
		return nil, err
	}
	calling, err := object(config["functionCallingConfig"], "mode", "allowedFunctionNames")
	if err != nil {
		return nil, err
	}
	mode := rawString(calling["mode"])
	var names []string
	if len(calling["allowedFunctionNames"]) != 0 {
		if json.Unmarshal(calling["allowedFunctionNames"], &names) != nil {
			return nil, errors.New("allowedFunctionNames must be an array of strings")
		}
	}
	switch mode {
	case "", "AUTO":
		if len(names) > 0 {
			return nil, errors.New("AUTO function calling config cannot restrict function names")
		}
		return "auto", nil
	case "NONE":
		if len(names) > 0 {
			return nil, errors.New("NONE function calling config cannot restrict function names")
		}
		return "none", nil
	case "ANY":
		switch len(names) {
		case 0:
			return "required", nil
		case 1:
			return map[string]any{"type": "function", "function": map[string]string{"name": names[0]}}, nil
		default:
			return nil, errors.New("tool_choice cannot allow more than one function")
		}
	default:
		return nil, fmt.Errorf("function calling mode %q cannot be converted", mode)
	}
}

// ChatToGeminiResponse converts one Chat completion into a GenerateContent
// response. Reasoning content becomes a thought part, function calls become
// functionCall parts, and additive provider envelope fields are dropped by
// the tolerant response parser.
func ChatToGeminiResponse(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	fields, err := responseObject(body, "id", "object", "created", "model", "choices", "usage")
	if err != nil {
		return nil, native.Usage{}, err
	}
	var objectType string
	_ = json.Unmarshal(fields["object"], &objectType)
	var choices []json.RawMessage
	if objectType != "chat.completion" || json.Unmarshal(fields["choices"], &choices) != nil || len(choices) != 1 {
		return nil, native.Usage{}, errors.New("Chat response requires a chat.completion object and one choice")
	}
	choice, err := responseObject(choices[0], "index", "message", "finish_reason")
	if err != nil {
		return nil, native.Usage{}, err
	}
	var index int
	if json.Unmarshal(choice["index"], &index) != nil || index != 0 {
		return nil, native.Usage{}, errors.New("Chat response choice index must be zero")
	}
	finishReason, err := chatFinishReasonToGemini(rawString(choice["finish_reason"]))
	if err != nil {
		return nil, native.Usage{}, err
	}
	message, err := responseObject(choice["message"], "role", "content", "tool_calls", "reasoning_content")
	if err != nil {
		return nil, native.Usage{}, err
	}
	var role string
	_ = json.Unmarshal(message["role"], &role)
	if role != "assistant" {
		return nil, native.Usage{}, errors.New("Chat response message must be assistant")
	}
	var parts []any
	if reasoning := rawString(message["reasoning_content"]); reasoning != "" {
		parts = append(parts, map[string]any{"text": reasoning, "thought": true})
	}
	blocks, err := chatTextBlocks(message["content"])
	if err != nil {
		return nil, native.Usage{}, err
	}
	for _, block := range blocks {
		if text, ok := block.(map[string]string); ok {
			parts = append(parts, map[string]string{"text": text["text"]})
		}
	}
	if len(message["tool_calls"]) != 0 {
		calls, err := chatToolCalls(message["tool_calls"])
		if err != nil {
			return nil, native.Usage{}, err
		}
		for _, call := range calls {
			input, err := chatToolArgsObject(call.Arguments)
			if err != nil {
				return nil, native.Usage{}, err
			}
			functionCall := map[string]any{"name": call.Name, "args": input}
			if call.ID != "" {
				functionCall["id"] = call.ID
			}
			parts = append(parts, map[string]any{"functionCall": functionCall})
		}
	}
	prompt, output, cached, seen := int64(0), int64(0), int64(0), false
	if nonNull(fields["usage"]) {
		_, err := responseObject(fields["usage"], "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens")
		if err != nil {
			return nil, native.Usage{}, err
		}
		var source struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			PromptDetails    struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			PromptCacheHitTokens int64 `json:"prompt_cache_hit_tokens"`
		}
		if json.Unmarshal(fields["usage"], &source) != nil || source.PromptTokens < 0 || source.CompletionTokens < 0 || source.PromptDetails.CachedTokens < 0 || source.PromptCacheHitTokens < 0 {
			return nil, native.Usage{}, errors.New("invalid Chat usage")
		}
		prompt, output = source.PromptTokens, source.CompletionTokens
		cached = max(source.PromptDetails.CachedTokens, source.PromptCacheHitTokens)
		seen = true
	}
	candidate := map[string]any{"index": 0, "finishReason": finishReason}
	if len(parts) > 0 {
		candidate["content"] = map[string]any{"role": "model", "parts": parts}
	}
	result := map[string]any{
		"candidates":   []any{candidate},
		"modelVersion": requestedModel,
	}
	if id := rawString(fields["id"]); id != "" {
		result["responseId"] = id
	}
	if seen {
		usageMetadata := map[string]any{"promptTokenCount": prompt, "candidatesTokenCount": output, "totalTokenCount": prompt + output}
		if cached > 0 {
			usageMetadata["cachedContentTokenCount"] = cached
		}
		result["usageMetadata"] = usageMetadata
	}
	converted, err := json.Marshal(result)
	return converted, native.Usage{Input: prompt, Output: output, Seen: seen, Complete: true}, err
}

// chatFinishReasonToGemini maps the Chat finish states onto Gemini finish
// reasons; a tool-call finish keeps the default STOP like Gemini does.
func chatFinishReasonToGemini(reason string) (string, error) {
	switch reason {
	case "stop", "tool_calls":
		return "STOP", nil
	case "length":
		return "MAX_TOKENS", nil
	case "content_filter":
		return "SAFETY", nil
	default:
		return "", fmt.Errorf("Chat finish reason %q cannot be converted", reason)
	}
}

// EnsureChatStreamOptions restores the stream intent on a converted Chat
// request body. Inbound protocols such as Gemini carry it in the URL action
// rather than the body, and streaming Chat upstreams need it in the body
// together with the usage report option. Idempotent for converters that
// already set the fields.
func EnsureChatStreamOptions(body []byte) ([]byte, error) {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil || request == nil {
		return nil, errors.New("converted Chat request must be a JSON object")
	}
	request["stream"] = true
	request["stream_options"] = map[string]bool{"include_usage": true}
	return json.Marshal(request)
}
