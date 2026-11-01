package bridge

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// responsesGemini converts the Responses client wire onto Gemini
// GenerateContent upstreams, and GenerateContent responses back onto the
// Responses wire. Responses is the richer wire, so the request conversion is a
// projection: items and fields without a GenerateContent representation are
// rejected before the upstream call. Remote image URLs are rejected because
// Gemini cannot fetch them and the gateway must not fetch on the client's
// behalf.
type responsesGemini struct{}

// Request converts the representable Responses subset onto the GenerateContent
// wire. The stream action is selected by the transport, so the body carries no
// stream flag.
func (responsesGemini) Request(body []byte, model string) ([]byte, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	fields, err := object(body, "model", "input", "instructions", "stream", "max_output_tokens",
		"temperature", "top_p", "tools", "tool_choice")
	if err != nil {
		return nil, err
	}
	if err := decode(fields["stream"], new(bool)); err != nil {
		return nil, errors.New("stream must be boolean")
	}
	var limit int64
	if err := decode(fields["max_output_tokens"], &limit); err != nil || limit < 0 {
		return nil, errors.New("max_output_tokens must not be negative")
	}
	config := map[string]any{}
	if limit > 0 {
		config["maxOutputTokens"] = limit
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
	request := map[string]any{}
	if len(fields["tools"]) != 0 {
		tools, err := responsesToolsToGemini(fields["tools"])
		if err != nil {
			return nil, err
		}
		request["tools"] = tools
	}
	if len(fields["tool_choice"]) != 0 {
		toolConfig, err := responsesToolChoiceToGemini(fields["tool_choice"])
		if err != nil {
			return nil, err
		}
		request["toolConfig"] = map[string]any{"functionCallingConfig": toolConfig}
	}
	if len(fields["instructions"]) != 0 {
		var instructions string
		if json.Unmarshal(fields["instructions"], &instructions) != nil {
			return nil, errors.New("instructions must be a string")
		}
		if instructions != "" {
			request["systemInstruction"] = map[string]any{"parts": []map[string]string{{"text": instructions}}}
		}
	}
	items, err := responsesInputArray(fields["input"])
	if err != nil {
		return nil, err
	}
	var contents []any
	toolNames := map[string]string{}
	for _, itemRaw := range items {
		item, err := object(itemRaw, "type", "role", "content", "call_id", "name", "arguments", "output", "id", "status", "summary", "encrypted_content", "signature", "thought_signature")
		if err != nil {
			return nil, err
		}
		switch rawString(item["type"]) {
		case "reasoning":
			// Visible reasoning history maps onto thought parts, and the
			// signature rides its own part so Gemini function-calling
			// coherence survives the round-trip. An encrypted blob belongs to
			// the vendor that issued it and has no Gemini carrier.
			reasoning, encrypted, err := responsesReasoningText(item)
			if err != nil {
				return nil, err
			}
			if encrypted != "" {
				return nil, errors.New("encrypted reasoning cannot cross to a Gemini upstream; use the Responses endpoint")
			}
			var thoughtParts []map[string]any
			if reasoning != "" {
				thoughtParts = append(thoughtParts, map[string]any{"text": reasoning, "thought": true})
			}
			if signature := rawString(item["signature"]); signature != "" {
				thoughtParts = append(thoughtParts, map[string]any{"thoughtSignature": signature})
			}
			if len(thoughtParts) > 0 {
				appendGeminiContent(&contents, "model", thoughtParts)
			}
		case "message":
			role := rawString(item["role"])
			switch role {
			case "system", "developer":
				return nil, errors.New("system message items must use instructions")
			case "user":
				parts, err := responsesUserPartsToGemini(item["content"])
				if err != nil {
					return nil, err
				}
				appendGeminiContent(&contents, "user", parts)
			case "assistant":
				parts, err := responsesAssistantPartsToGemini(item["content"])
				if err != nil {
					return nil, err
				}
				appendGeminiContent(&contents, "model", parts)
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
			if existing, ok := toolNames[callID]; ok && existing != name {
				return nil, errors.New("function_call id maps to conflicting function names")
			}
			toolNames[callID] = name
			functionCall := map[string]any{"name": name, "args": input, "id": callID}
			// The thought signature Gemini attaches to function calls must be
			// echoed on replay for thinking-model coherence.
			if signature := rawString(item["thought_signature"]); signature != "" {
				functionCall["thoughtSignature"] = signature
			}
			appendGeminiContent(&contents, "model", []map[string]any{{"functionCall": functionCall}})
		case "function_call_output":
			callID, output := rawString(item["call_id"]), rawString(item["output"])
			if callID == "" || output == "" {
				return nil, errors.New("Responses function_call_output requires a call_id and output")
			}
			name := toolNames[callID]
			if name == "" {
				return nil, errors.New("function_call_output references an unknown call_id")
			}
			response, err := geminiToolResponseObject(output)
			if err != nil {
				return nil, err
			}
			functionResponse := map[string]any{"id": callID, "name": name, "response": response}
			appendGeminiContent(&contents, "user", []map[string]any{{"functionResponse": functionResponse}})
		default:
			return nil, fmt.Errorf("Responses input item %q cannot be converted", rawString(item["type"]))
		}
	}
	if len(contents) == 0 {
		return nil, errors.New("Responses input produced no convertible messages")
	}
	request["contents"] = contents
	if len(config) > 0 {
		request["generationConfig"] = config
	}
	return json.Marshal(request)
}

// responsesUserPartsToGemini converts user message content onto GenerateContent
// parts.
func responsesUserPartsToGemini(raw json.RawMessage) ([]map[string]any, error) {
	var plain string
	if len(raw) != 0 && json.Unmarshal(raw, &plain) == nil {
		if plain == "" {
			return nil, errors.New("Responses message content is empty")
		}
		return []map[string]any{{"text": plain}}, nil
	}
	parts, err := responsesContentParts(raw, "input_text", "input_image")
	if err != nil {
		return nil, err
	}
	var converted []map[string]any
	for _, part := range parts {
		switch rawString(part["type"]) {
		case "input_text":
			if len(part) != 2 {
				return nil, errors.New("input_text part has fields Gemini cannot represent")
			}
			converted = append(converted, map[string]any{"text": rawString(part["text"])})
		case "input_image":
			media, err := responsesImageToGemini(part["image_url"])
			if err != nil {
				return nil, err
			}
			converted = append(converted, media)
		}
	}
	if len(converted) == 0 {
		return nil, errors.New("Responses message content is empty")
	}
	return converted, nil
}

// responsesAssistantPartsToGemini converts assistant message content onto
// GenerateContent parts. A refusal part is display text to the recipient, so
// it converts as ordinary text.
func responsesAssistantPartsToGemini(raw json.RawMessage) ([]map[string]any, error) {
	var plain string
	if len(raw) != 0 && json.Unmarshal(raw, &plain) == nil {
		if plain == "" {
			return nil, errors.New("Responses message content is empty")
		}
		return []map[string]any{{"text": plain}}, nil
	}
	parts, err := responsesContentParts(raw, "output_text", "refusal")
	if err != nil {
		return nil, err
	}
	var converted []map[string]any
	for _, part := range parts {
		switch rawString(part["type"]) {
		case "output_text":
			if len(part) != 2 {
				return nil, errors.New("output_text part has fields Gemini cannot represent")
			}
			converted = append(converted, map[string]any{"text": rawString(part["text"])})
		case "refusal":
			if len(part) != 2 {
				return nil, errors.New("refusal part has fields Gemini cannot represent")
			}
			converted = append(converted, map[string]any{"text": rawString(part["refusal"])})
		}
	}
	if len(converted) == 0 {
		return nil, errors.New("Responses message content is empty")
	}
	return converted, nil
}

// responsesImageToGemini converts an input_image part onto a Gemini inline
// data part. Only base64 data URLs convert: remote URLs cannot be fetched.
func responsesImageToGemini(raw json.RawMessage) (map[string]any, error) {
	var address string
	if json.Unmarshal(raw, &address) != nil || address == "" {
		return nil, errors.New("input_image part requires an image_url string")
	}
	if len(address) <= 5 || address[:5] != "data:" {
		return nil, errors.New("Gemini conversion requires base64 image data; remote image URLs cannot be fetched")
	}
	mediaType, data, err := parseImageDataURL(address)
	if err != nil {
		return nil, err
	}
	return geminiInlineData(mediaType, data), nil
}

// responsesToolsToGemini converts Responses function tools onto Gemini
// function declarations. strict has no Gemini equivalent and is rejected.
func responsesToolsToGemini(raw json.RawMessage) ([]any, error) {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return nil, errors.New("tools must be an array")
	}
	var declarations []any
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
		declaration := map[string]any{"name": name}
		if len(tool["description"]) != 0 {
			declaration["description"] = json.RawMessage(tool["description"])
		}
		if len(tool["parameters"]) != 0 && string(tool["parameters"]) != "null" {
			var schema map[string]any
			if json.Unmarshal(tool["parameters"], &schema) != nil || schema == nil {
				return nil, errors.New("function tool parameters must be a JSON object")
			}
			declaration["parameters"] = json.RawMessage(tool["parameters"])
		}
		declarations = append(declarations, declaration)
	}
	if len(declarations) == 0 {
		return nil, errors.New("tools must not be empty")
	}
	return []any{map[string]any{"functionDeclarations": declarations}}, nil
}

// responsesToolChoiceToGemini converts the Responses tool_choice values.
func responsesToolChoiceToGemini(raw json.RawMessage) (map[string]any, error) {
	var choice string
	if json.Unmarshal(raw, &choice) == nil {
		switch choice {
		case "auto":
			return map[string]any{"mode": "AUTO"}, nil
		case "none":
			return map[string]any{"mode": "NONE"}, nil
		case "required":
			return map[string]any{"mode": "ANY"}, nil
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
	return map[string]any{"mode": "ANY", "allowedFunctionNames": []string{name}}, nil
}

// Response converts a non-streaming GenerateContent response onto the Responses
// wire. Thought parts and their thought signatures map onto reasoning items so
// thinking text and the signature Gemini requires for multi-turn function
// calling both round-trip.
func (responsesGemini) Response(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	fields, extras, err := responseObject(body, geminiResponseFields...)
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata := newProviderMetadata(extras)
	responseID := rawString(fields["responseId"])
	if responseID == "" {
		generated, err := newStreamID("resp_")
		if err != nil {
			return nil, native.Usage{}, err
		}
		responseID = generated
	}
	var rawCandidates []json.RawMessage
	if json.Unmarshal(fields["candidates"], &rawCandidates) != nil || len(rawCandidates) != 1 {
		return nil, native.Usage{}, errors.New("Gemini response requires one candidate")
	}
	candidate, candidateExtras, err := responseObject(rawCandidates[0], geminiCandidateFields...)
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata.absorb(candidateExtras)
	if err := geminiCandidateIndex(candidate["index"]); err != nil {
		return nil, native.Usage{}, err
	}
	content, contentExtras, err := responseObject(candidate["content"], "role", "parts")
	if err != nil {
		return nil, native.Usage{}, err
	}
	metadata.absorb(contentExtras)
	if role := rawString(content["role"]); role != "" && role != "model" {
		return nil, native.Usage{}, errors.New("Gemini response content role must be model")
	}
	var rawParts []json.RawMessage
	if json.Unmarshal(content["parts"], &rawParts) != nil || len(rawParts) == 0 {
		return nil, native.Usage{}, errors.New("Gemini response requires content parts")
	}
	output := []any{}
	messageOpen := false
	openMessage := func() {
		if messageOpen {
			return
		}
		messageOpen = true
		output = append(output, map[string]any{
			"type": "message", "id": newResponsesItemID("msg_"), "status": "completed", "role": "assistant",
			"content": []any{},
		})
	}
	// reasoningItem returns the open reasoning item, creating it on demand so
	// thought text and signatures land in one place.
	var openReasoning map[string]any
	reasoningItem := func() map[string]any {
		if openReasoning == nil {
			openReasoning = map[string]any{
				"type": "reasoning", "id": newResponsesItemID("rsn_"), "summary": []any{},
			}
			output = append(output, openReasoning)
			messageOpen = false
		}
		return openReasoning
	}
	closeReasoning := func() {
		openReasoning = nil
	}
	for _, raw := range rawParts {
		part, err := parseGeminiResponsePart(raw)
		if err != nil {
			return nil, native.Usage{}, err
		}
		switch part.kind {
		case geminiPartText:
			if part.signature != "" {
				item := reasoningItem()
				item["signature"] = part.signature
			}
			closeReasoning()
			openMessage()
			message := output[len(output)-1].(map[string]any)
			message["content"] = append(message["content"].([]any),
				map[string]any{"type": "output_text", "text": part.text, "annotations": []any{}})
		case geminiPartThought:
			item := reasoningItem()
			if part.text != "" {
				item["summary"] = append(item["summary"].([]any), map[string]string{"type": "summary_text", "text": part.text})
			}
			if part.signature != "" {
				item["signature"] = part.signature
			}
		case geminiPartSignature:
			item := reasoningItem()
			item["signature"] = part.signature
		case geminiPartFunctionCall:
			closeReasoning()
			messageOpen = false
			callID := part.callID
			if callID == "" {
				generated, err := newStreamID("call_")
				if err != nil {
					return nil, native.Usage{}, err
				}
				callID = generated
			}
			item := map[string]any{
				"type": "function_call", "id": newResponsesItemID("fc_"), "status": "completed",
				"call_id": callID, "name": part.name, "arguments": string(part.args),
			}
			if part.signature != "" {
				item["thought_signature"] = part.signature
			}
			output = append(output, item)
		}
	}
	stop := rawString(candidate["finishReason"])
	if stop == "" {
		return nil, native.Usage{}, errors.New("Gemini response requires a finish reason")
	}
	status := "completed"
	var incompleteDetails any
	switch stop {
	case "STOP":
	case "MAX_TOKENS":
		status = "incomplete"
		incompleteDetails = map[string]string{"reason": "max_output_tokens"}
	default:
		return nil, native.Usage{}, fmt.Errorf("Gemini finish reason %q cannot be converted", stop)
	}
	usage, err := geminiUsageTokens(fields["usageMetadata"], true)
	if err != nil {
		return nil, native.Usage{}, err
	}
	usage.Complete = true
	result := map[string]any{
		"id": responseID, "object": "response", "created_at": streamTimestamp(),
		"status": status, "model": requestedModel, "output": output,
	}
	if usage.Seen {
		result["usage"] = responsesUsageEnvelope(usage)
	}
	if incompleteDetails != nil {
		result["incomplete_details"] = incompleteDetails
	}
	if gemini := geminiProviderMetadata(fields, candidate); len(gemini) > 0 {
		if m := metadata.value(); m != nil {
			m["gemini"] = gemini
		} else {
			metadata.set("gemini", gemini)
		}
	}
	if m := metadata.value(); m != nil {
		result["provider_metadata"] = m
	}
	converted, err := json.Marshal(result)
	return converted, usage, err
}
