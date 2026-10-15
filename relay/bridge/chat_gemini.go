package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/infinmalum/one-gateway/relay/native"
)

// ChatToGemini converts the Chat Completions subset that Gemini
// GenerateContent can represent: text, function tools and calls, base64
// images and audio input, and thought-free responses. Fields without a
// Gemini representation fail before the upstream call. Streaming requests
// convert their body; the upstream stream action is selected by the
// transport.
func ChatToGemini(body []byte, model string) ([]byte, error) {
	head, err := parseChatRequestHead(body, model, "stop")
	if err != nil {
		return nil, err
	}
	fields := head.fields
	config := map[string]any{}
	if head.limit > 0 {
		config["maxOutputTokens"] = head.limit
	}
	for _, pair := range [][2]string{{"temperature", "temperature"}, {"top_p", "topP"}} {
		if len(fields[pair[0]]) != 0 {
			var value float64
			if json.Unmarshal(fields[pair[0]], &value) != nil {
				return nil, fmt.Errorf("%s must be a number", pair[0])
			}
			config[pair[1]] = value
		}
	}
	if len(fields["stop"]) != 0 {
		stops, err := chatStopSequences(fields["stop"])
		if err != nil {
			return nil, err
		}
		if len(stops) > 5 {
			return nil, errors.New("stop must contain at most five strings")
		}
		config["stopSequences"] = stops
	}
	request := map[string]any{}
	if len(fields["tools"]) != 0 {
		tools, err := chatToolsToGemini(fields["tools"])
		if err != nil {
			return nil, err
		}
		request["tools"] = tools
	}
	if len(fields["tool_choice"]) != 0 {
		toolConfig, err := chatToolChoiceToGemini(fields["tool_choice"])
		if err != nil {
			return nil, err
		}
		request["toolConfig"] = map[string]any{"functionCallingConfig": toolConfig}
	}
	var contents []any
	var system []string
	toolNames := map[string]string{}
	for _, raw := range head.messages {
		message, err := object(raw, "role", "content", "tool_calls", "tool_call_id")
		if err != nil {
			return nil, fmt.Errorf("invalid Chat message: %w", err)
		}
		var role string
		_ = json.Unmarshal(message["role"], &role)
		switch role {
		case "system":
			if len(contents) != 0 {
				return nil, errors.New("system messages must precede conversation messages")
			}
			parts, err := geminiTextParts(message["content"])
			if err != nil {
				return nil, err
			}
			for _, part := range parts {
				text, _ := part["text"].(string)
				system = append(system, text)
			}
		case "user":
			if len(message["tool_calls"]) != 0 {
				return nil, errors.New("user messages cannot contain tool_calls")
			}
			if len(message["tool_call_id"]) != 0 {
				return nil, errors.New("tool_call_id is only valid on tool messages")
			}
			parts, err := chatUserPartsToGemini(message["content"])
			if err != nil {
				return nil, err
			}
			appendGeminiContent(&contents, "user", parts)
		case "assistant":
			if len(message["tool_call_id"]) != 0 {
				return nil, errors.New("tool_call_id is only valid on tool messages")
			}
			parts, err := geminiTextParts(message["content"])
			if err != nil {
				return nil, err
			}
			var calls []chatToolCall
			if len(message["tool_calls"]) != 0 {
				if calls, err = chatToolCalls(message["tool_calls"]); err != nil {
					return nil, err
				}
			}
			if len(calls) > 0 {
				parts = keepNonEmptyText(parts)
			}
			for _, call := range calls {
				part, err := chatToolCallToGemini(call, toolNames)
				if err != nil {
					return nil, err
				}
				parts = append(parts, part)
			}
			if len(parts) == 0 {
				return nil, errors.New("Chat message content is empty")
			}
			appendGeminiContent(&contents, "model", parts)
		case "tool":
			if len(message["tool_calls"]) != 0 {
				return nil, errors.New("tool messages cannot contain tool_calls")
			}
			part, err := chatToolResultToGemini(message, toolNames)
			if err != nil {
				return nil, err
			}
			appendGeminiContent(&contents, "user", []map[string]any{part})
		default:
			return nil, fmt.Errorf("Chat role %q cannot be converted to Gemini", role)
		}
	}
	if len(contents) == 0 {
		return nil, errors.New("conversation messages are required")
	}
	request["contents"] = contents
	if len(system) > 0 {
		request["systemInstruction"] = map[string]any{"parts": []map[string]string{{"text": joinSystem(system)}}}
	}
	if len(config) > 0 {
		request["generationConfig"] = config
	}
	return json.Marshal(request)
}

func chatToolCallToGemini(call chatToolCall, toolNames map[string]string) (map[string]any, error) {
	input, err := chatToolArgsObject(call.Arguments)
	if err != nil {
		return nil, err
	}
	if existing, ok := toolNames[call.ID]; ok && existing != call.Name {
		return nil, errors.New("tool_call id maps to conflicting function names")
	}
	toolNames[call.ID] = call.Name
	functionCall := map[string]any{"name": call.Name, "args": input}
	if call.ID != "" {
		functionCall["id"] = call.ID
	}
	return map[string]any{"functionCall": functionCall}, nil
}

func chatToolResultToGemini(message map[string]json.RawMessage, toolNames map[string]string) (map[string]any, error) {
	var id, content string
	_ = json.Unmarshal(message["tool_call_id"], &id)
	if id == "" || json.Unmarshal(message["content"], &content) != nil {
		return nil, errors.New("tool messages require tool_call_id and text content")
	}
	name := toolNames[id]
	if name == "" {
		return nil, errors.New("tool message references an unknown tool_call id")
	}
	response, err := geminiToolResponseObject(content)
	if err != nil {
		return nil, err
	}
	return map[string]any{"functionResponse": map[string]any{"id": id, "name": name, "response": response}}, nil
}

func keepNonEmptyText(parts []map[string]any) []map[string]any {
	var kept []map[string]any
	for _, part := range parts {
		if part["text"] != "" {
			kept = append(kept, part)
		}
	}
	return kept
}

func chatUserPartsToGemini(raw json.RawMessage) ([]map[string]any, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []map[string]any{{"text": text}}, nil
	}
	var rawParts []json.RawMessage
	if json.Unmarshal(raw, &rawParts) != nil || len(rawParts) == 0 {
		return nil, errors.New("Gemini conversion requires text content")
	}
	var parts []map[string]any
	for _, rawPart := range rawParts {
		part, err := object(rawPart, "type", "text", "image_url", "input_audio")
		if err != nil {
			return nil, err
		}
		var kind string
		_ = json.Unmarshal(part["type"], &kind)
		switch kind {
		case "text":
			var value string
			if len(part) != 2 || json.Unmarshal(part["text"], &value) != nil {
				return nil, errors.New("Chat text part requires text")
			}
			parts = append(parts, map[string]any{"text": value})
		case "image_url":
			if len(part) != 2 {
				return nil, errors.New("Chat image part requires only image_url")
			}
			media, err := chatImageToGemini(part["image_url"])
			if err != nil {
				return nil, err
			}
			parts = append(parts, media)
		case "input_audio":
			if len(part) != 2 {
				return nil, errors.New("Chat audio part requires only input_audio")
			}
			media, err := chatAudioToGemini(part["input_audio"])
			if err != nil {
				return nil, err
			}
			parts = append(parts, media)
		default:
			return nil, fmt.Errorf("Chat content part %q cannot be converted", kind)
		}
	}
	return parts, nil
}

func geminiTextParts(raw json.RawMessage) ([]map[string]any, error) {
	if text, ok := chatContentString(raw); ok {
		return []map[string]any{{"text": text}}, nil
	}
	texts, err := chatTextPartList(raw)
	if err != nil || len(texts) == 0 {
		return nil, errors.New("Gemini conversion requires text content")
	}
	parts := make([]map[string]any, 0, len(texts))
	for _, text := range texts {
		parts = append(parts, map[string]any{"text": text})
	}
	return parts, nil
}

// GeminiToChat keeps provider metadata in a named response extension while
// rejecting response content and finish states Chat cannot represent.
// Function calls become tool calls with the upstream call ID when present;
// thought parts have no Chat representation and fail.
func GeminiToChat(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	fields, err := object(body, geminiResponseFields...)
	if err != nil {
		return nil, native.Usage{}, err
	}
	var id string
	_ = json.Unmarshal(fields["responseId"], &id)
	if id == "" {
		generated, err := newStreamID(chatCompletionIDPrefix)
		if err != nil {
			return nil, native.Usage{}, err
		}
		id = generated
	}
	var rawCandidates []json.RawMessage
	if json.Unmarshal(fields["candidates"], &rawCandidates) != nil || len(rawCandidates) != 1 {
		return nil, native.Usage{}, errors.New("Gemini response requires one candidate")
	}
	candidate, err := object(rawCandidates[0], geminiCandidateFields...)
	if err != nil {
		return nil, native.Usage{}, err
	}
	if err := geminiCandidateIndex(candidate["index"]); err != nil {
		return nil, native.Usage{}, err
	}
	var finish string
	_ = json.Unmarshal(candidate["finishReason"], &finish)
	content, err := object(candidate["content"], "role", "parts")
	if err != nil {
		return nil, native.Usage{}, err
	}
	var role string
	_ = json.Unmarshal(content["role"], &role)
	if role != "" && role != "model" {
		return nil, native.Usage{}, errors.New("Gemini response content role must be model")
	}
	var rawParts []json.RawMessage
	if json.Unmarshal(content["parts"], &rawParts) != nil || len(rawParts) == 0 {
		return nil, native.Usage{}, errors.New("Gemini response requires content parts")
	}
	var text string
	var toolCalls []any
	for _, raw := range rawParts {
		part, err := parseGeminiResponsePart(raw)
		if err != nil {
			return nil, native.Usage{}, err
		}
		switch part.kind {
		case geminiPartText:
			text += part.text
		case geminiPartFunctionCall:
			callID := part.callID
			if callID == "" {
				generated, err := newStreamID("call_")
				if err != nil {
					return nil, native.Usage{}, err
				}
				callID = generated
			}
			toolCalls = append(toolCalls, map[string]any{
				"id": callID, "type": "function",
				"function": map[string]string{"name": part.name, "arguments": string(part.args)},
			})
		default:
			return nil, native.Usage{}, errors.New("Gemini response contains a part Chat cannot represent")
		}
	}
	if text == "" && len(toolCalls) == 0 {
		return nil, native.Usage{}, errors.New("Gemini response requires content")
	}
	chatFinish, err := mapGeminiFinishReason(finish, len(toolCalls) > 0)
	if err != nil {
		return nil, native.Usage{}, err
	}
	prompt, output, cached, err := geminiUsage(fields["usageMetadata"], true)
	if err != nil {
		return nil, native.Usage{}, err
	}
	usage := native.Usage{Input: prompt, Output: output, Seen: true, Complete: true}
	message := map[string]any{"role": "assistant", "content": nil}
	if text != "" {
		message["content"] = text
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	result := map[string]any{
		"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": requestedModel,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": chatFinish}},
		"usage":   map[string]any{"prompt_tokens": usage.Input, "completion_tokens": usage.Output, "total_tokens": usage.Input + usage.Output},
	}
	if cached > 0 {
		result["usage"].(map[string]any)["prompt_tokens_details"] = map[string]int64{"cached_tokens": cached}
	}
	result["provider_metadata"] = map[string]any{"gemini": geminiProviderMetadata(fields, candidate)}
	converted, err := json.Marshal(result)
	return converted, usage, err
}
