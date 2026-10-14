package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/infinmalum/one-gateway/relay/native"
)

// chatResponses converts Chat Completions requests onto the OpenAI Responses
// wire format. Subscription profiles serve streaming responses exclusively, so
// the upstream request always forwards with stream=true; synchronous clients
// receive the aggregated stream as one JSON response. Registry instances are
// request-scoped, so the captured stream_options flag survives until Stream.
type chatResponses struct {
	includeUsage bool
}

// ChatToResponses converts the representable Chat Completions subset. Fields
// without a Responses equivalent (stop, n, penalties, logprobs) are rejected
// by the strict field whitelist; profile-level restrictions such as
// temperature are mapped faithfully and rejected by the account validator.
func (r *chatResponses) Request(body []byte, model string) ([]byte, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	fields, err := object(body, "model", "messages", "stream", "stream_options", "max_tokens",
		"max_completion_tokens", "temperature", "top_p", "tools", "tool_choice",
		"parallel_tool_calls", "response_format", "metadata", "user")
	if err != nil {
		return nil, err
	}
	var stream bool
	if err := decode(fields["stream"], &stream); err != nil {
		return nil, errors.New("stream must be boolean")
	}
	r.includeUsage = false
	if nonNull(fields["stream_options"]) {
		options, err := object(fields["stream_options"], "include_usage")
		if err != nil {
			return nil, fmt.Errorf("invalid stream_options: %w", err)
		}
		if err := decode(options["include_usage"], &r.includeUsage); err != nil {
			return nil, errors.New("stream_options include_usage must be boolean")
		}
	}
	var maxTokens, maxCompletion int64
	if err := decode(fields["max_tokens"], &maxTokens); err != nil || maxTokens < 0 {
		return nil, errors.New("max_tokens must not be negative")
	}
	if err := decode(fields["max_completion_tokens"], &maxCompletion); err != nil || maxCompletion < 0 {
		return nil, errors.New("max_completion_tokens must not be negative")
	}
	if maxTokens > 0 && maxCompletion > 0 && maxTokens != maxCompletion {
		return nil, errors.New("conflicting Chat token limits")
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(fields["messages"], &messages); err != nil || len(messages) == 0 {
		return nil, errors.New("messages must be a non-empty array")
	}
	request := map[string]any{"model": model, "stream": true, "store": false}
	var instructions []string
	var input []any
	for _, raw := range messages {
		message, err := object(raw, "role", "content", "tool_calls", "tool_call_id")
		if err != nil {
			return nil, fmt.Errorf("invalid Chat message: %w", err)
		}
		var role string
		if err := json.Unmarshal(message["role"], &role); err != nil {
			return nil, errors.New("Chat message role is required")
		}
		switch role {
		case "system", "developer":
			if len(input) > 0 {
				return nil, errors.New("system and developer messages must precede conversation messages")
			}
			text, err := chatTextContent(message["content"])
			if err != nil {
				return nil, fmt.Errorf("invalid Chat system message: %w", err)
			}
			instructions = append(instructions, text)
		case "user":
			if len(message["tool_call_id"]) != 0 || len(message["tool_calls"]) != 0 {
				return nil, errors.New("tool fields are only valid on assistant and tool messages")
			}
			parts, err := chatInputParts(message["content"])
			if err != nil {
				return nil, err
			}
			input = append(input, map[string]any{"type": "message", "role": "user", "content": parts})
		case "assistant":
			if len(message["tool_call_id"]) != 0 {
				return nil, errors.New("tool_call_id is only valid on tool messages")
			}
			if nonNull(message["content"]) {
				text, err := chatTextContent(message["content"])
				if err != nil {
					return nil, fmt.Errorf("invalid Chat assistant message: %w", err)
				}
				if text != "" {
					input = append(input, map[string]any{"type": "message", "role": "assistant",
						"content": []any{map[string]string{"type": "output_text", "text": text}}})
				}
			}
			if len(message["tool_calls"]) != 0 {
				items, err := chatFunctionCalls(message["tool_calls"])
				if err != nil {
					return nil, err
				}
				input = append(input, items...)
			}
		case "tool":
			if len(message["tool_calls"]) != 0 {
				return nil, errors.New("tool messages cannot contain tool_calls")
			}
			var callID string
			_ = json.Unmarshal(message["tool_call_id"], &callID)
			if callID == "" {
				return nil, errors.New("tool messages require tool_call_id")
			}
			output, err := chatTextContent(message["content"])
			if err != nil {
				return nil, fmt.Errorf("invalid Chat tool message: %w", err)
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": callID, "output": output})
		default:
			return nil, fmt.Errorf("Chat role %q is not supported by Responses conversion", role)
		}
	}
	if len(input) == 0 {
		return nil, errors.New("Chat messages produced no convertible input")
	}
	request["input"] = input
	if len(instructions) > 0 {
		request["instructions"] = joinSystem(instructions)
	}
	if limit := max(maxTokens, maxCompletion); limit > 0 {
		request["max_output_tokens"] = limit
	}
	for _, key := range []string{"temperature", "top_p", "metadata", "user"} {
		if nonNull(fields[key]) {
			request[key] = json.RawMessage(fields[key])
		}
	}
	if nonNull(fields["parallel_tool_calls"]) {
		var parallel bool
		if err := decode(fields["parallel_tool_calls"], &parallel); err != nil {
			return nil, errors.New("parallel_tool_calls must be boolean")
		}
		request["parallel_tool_calls"] = parallel
	}
	if nonNull(fields["tools"]) {
		converted, err := chatFunctionTools(fields["tools"])
		if err != nil {
			return nil, err
		}
		request["tools"] = converted
	}
	if nonNull(fields["tool_choice"]) {
		choice, err := chatToolChoice(fields["tool_choice"])
		if err != nil {
			return nil, err
		}
		request["tool_choice"] = choice
	}
	if nonNull(fields["response_format"]) {
		text, err := chatResponseFormat(fields["response_format"])
		if err != nil {
			return nil, err
		}
		if text != nil {
			request["text"] = text
		}
	}
	return json.Marshal(request)
}

// chatTextContent reads string or text-only content parts.
func chatTextContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", errors.New("content is required")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return "", errors.New("content must be text or text parts")
	}
	joined := ""
	for _, part := range parts {
		item, err := object(part, "type", "text")
		if err != nil {
			return "", err
		}
		if rawString(item["type"]) != "text" {
			return "", fmt.Errorf("Chat content part %q cannot be converted", rawString(item["type"]))
		}
		joined += rawString(item["text"])
	}
	return joined, nil
}

// chatInputParts converts user content parts. Images pass as data or remote
// URLs; audio and video have no Responses representation and fail explicitly.
func chatInputParts(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("Chat message content is required")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []any{map[string]string{"type": "input_text", "text": text}}, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil, errors.New("Chat content must be text or content parts")
	}
	var result []any
	for _, part := range parts {
		item, err := object(part, "type", "text", "image_url")
		if err != nil {
			return nil, err
		}
		switch rawString(item["type"]) {
		case "text":
			result = append(result, map[string]string{"type": "input_text", "text": rawString(item["text"])})
		case "image_url":
			image := map[string]any{"type": "input_image"}
			var url string
			if json.Unmarshal(item["image_url"], &url) == nil && url != "" {
				image["image_url"] = url
			} else {
				field, err := object(item["image_url"], "url", "detail")
				if err != nil {
					return nil, fmt.Errorf("invalid Chat image part: %w", err)
				}
				if url = rawString(field["url"]); url == "" {
					return nil, errors.New("Chat image part requires a URL")
				}
				image["image_url"] = url
				if nonNull(field["detail"]) {
					image["detail"] = json.RawMessage(field["detail"])
				}
			}
			result = append(result, image)
		default:
			return nil, fmt.Errorf("Chat content part %q cannot be converted", rawString(item["type"]))
		}
	}
	return result, nil
}

func chatFunctionCalls(raw json.RawMessage) ([]any, error) {
	var calls []json.RawMessage
	if json.Unmarshal(raw, &calls) != nil {
		return nil, errors.New("tool_calls must be an array")
	}
	var result []any
	for _, callRaw := range calls {
		call, err := object(callRaw, "id", "type", "function")
		if err != nil {
			return nil, err
		}
		var id, kind string
		_ = json.Unmarshal(call["id"], &id)
		_ = json.Unmarshal(call["type"], &kind)
		if id == "" || kind != "function" {
			return nil, errors.New("only named function tool calls can be converted")
		}
		function, err := object(call["function"], "name", "arguments")
		if err != nil {
			return nil, err
		}
		var name, arguments string
		_ = json.Unmarshal(function["name"], &name)
		_ = json.Unmarshal(function["arguments"], &arguments)
		if name == "" {
			return nil, errors.New("function tool call name is required")
		}
		if json.Valid([]byte(arguments)) {
			result = append(result, map[string]any{"type": "function_call", "call_id": id, "name": name, "arguments": arguments})
		} else {
			return nil, errors.New("function tool arguments must be valid JSON")
		}
	}
	return result, nil
}

func chatFunctionTools(raw json.RawMessage) ([]any, error) {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return nil, errors.New("tools must be an array")
	}
	var result []any
	for _, toolRaw := range tools {
		tool, err := object(toolRaw, "type", "function")
		if err != nil {
			return nil, err
		}
		if rawString(tool["type"]) != "function" {
			return nil, errors.New("only function tools can be converted")
		}
		function, err := object(tool["function"], "name", "description", "parameters", "strict")
		if err != nil {
			return nil, err
		}
		var name string
		_ = json.Unmarshal(function["name"], &name)
		if name == "" {
			return nil, errors.New("function tool name is required")
		}
		converted := map[string]any{"type": "function", "name": name}
		if nonNull(function["description"]) {
			converted["description"] = json.RawMessage(function["description"])
		}
		if nonNull(function["parameters"]) {
			var schema map[string]any
			if json.Unmarshal(function["parameters"], &schema) != nil || schema == nil {
				return nil, errors.New("function tool parameters must be a JSON object")
			}
			converted["parameters"] = json.RawMessage(function["parameters"])
		}
		if nonNull(function["strict"]) {
			var strict bool
			if err := decode(function["strict"], &strict); err != nil {
				return nil, errors.New("function tool strict must be boolean")
			}
			converted["strict"] = strict
		}
		result = append(result, converted)
	}
	return result, nil
}

func chatToolChoice(raw json.RawMessage) (any, error) {
	var choice string
	if json.Unmarshal(raw, &choice) == nil {
		switch choice {
		case "auto", "none", "required":
			return choice, nil
		default:
			return nil, fmt.Errorf("tool_choice %q cannot be converted", choice)
		}
	}
	choiceObject, err := object(raw, "type", "function")
	if err != nil {
		return nil, err
	}
	if rawString(choiceObject["type"]) != "function" {
		return nil, errors.New("only named function tool_choice can be converted")
	}
	function, err := object(choiceObject["function"], "name")
	if err != nil {
		return nil, err
	}
	var name string
	_ = json.Unmarshal(function["name"], &name)
	if name == "" {
		return nil, errors.New("named function tool_choice requires a name")
	}
	return map[string]string{"type": "function", "name": name}, nil
}

// chatResponseFormat maps response_format onto the Responses text.format
// object. Plain text and auto are the defaults and are omitted.
func chatResponseFormat(raw json.RawMessage) (map[string]any, error) {
	format, err := object(raw, "type", "json_schema")
	if err != nil {
		return nil, err
	}
	switch rawString(format["type"]) {
	case "text", "auto", "":
		return nil, nil
	case "json_object":
		return map[string]any{"format": map[string]any{"type": "json_object"}}, nil
	case "json_schema":
		schema, err := object(format["json_schema"], "name", "description", "schema", "strict")
		if err != nil {
			return nil, fmt.Errorf("invalid response_format json_schema: %w", err)
		}
		var name string
		_ = json.Unmarshal(schema["name"], &name)
		if name == "" || !nonNull(schema["schema"]) {
			return nil, errors.New("response_format json_schema requires a name and schema")
		}
		converted := map[string]any{"type": "json_schema", "name": name, "schema": json.RawMessage(schema["schema"])}
		if nonNull(schema["description"]) {
			converted["description"] = json.RawMessage(schema["description"])
		}
		if nonNull(schema["strict"]) {
			var strict bool
			if err := decode(schema["strict"], &strict); err != nil {
				return nil, errors.New("response_format strict must be boolean")
			}
			converted["strict"] = strict
		}
		return map[string]any{"format": converted}, nil
	default:
		return nil, fmt.Errorf("response_format %q cannot be converted", rawString(format["type"]))
	}
}

// Response converts a non-streaming upstream body: either a Responses JSON
// object or, because the forwarded request always streams, the raw event
// stream aggregated into its terminal response.
func (r *chatResponses) Response(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] != '{' {
		aggregated, err := aggregateResponsesEvents(newSSEReader(bytes.NewReader(trimmed)))
		if err != nil {
			return nil, native.Usage{}, err
		}
		body = aggregated
	}
	return responsesToChat(body, requestedModel)
}

// aggregateResponsesEvents reduces a Responses event stream to its terminal
// response object. The lifecycle caps the buffered stream, and newSSEReader
// caps each event, so aggregation memory is bounded.
func aggregateResponsesEvents(reader *sseReader) ([]byte, error) {
	for {
		data, err := reader.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if string(data) == "[DONE]" {
			break
		}
		var event struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
			Message  string          `json:"message"`
		}
		if json.Unmarshal(data, &event) != nil || event.Type == "" {
			return nil, errors.New("invalid Responses event")
		}
		switch event.Type {
		case "response.completed", "response.incomplete":
			if len(event.Response) == 0 {
				return nil, errors.New("Responses terminal event requires a response object")
			}
			return event.Response, nil
		case "response.failed", "error":
			message := event.Message
			if event.Type == "response.failed" {
				message = responsesFailureMessage(event.Response)
			}
			if message == "" {
				message = "upstream Responses stream failed"
			}
			return nil, errors.New(message)
		}
	}
	return nil, errors.New("Responses stream ended before response.completed")
}

func responsesFailureMessage(raw json.RawMessage) string {
	var failed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &failed) == nil && failed.Error != nil {
		return failed.Error.Message
	}
	return ""
}

// responsesToChat converts a final Responses object. The envelope is parsed
// tolerantly because provider metadata carries no conversation content;
// output items and content parts are strict because they do.
func responsesToChat(body []byte, requestedModel string) ([]byte, native.Usage, error) {
	var response struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
		Status    string `json:"status"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type        string            `json:"type"`
				Text        string            `json:"text"`
				Refusal     string            `json:"refusal"`
				Annotations []json.RawMessage `json:"annotations"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &response) != nil || response.ID == "" {
		return nil, native.Usage{}, errors.New("invalid Responses response")
	}
	if response.Error != nil && response.Error.Message != "" {
		return nil, native.Usage{}, fmt.Errorf("upstream Responses request failed: %s", response.Error.Message)
	}
	var finish string
	switch response.Status {
	case "completed":
		finish = "stop"
	case "incomplete":
		reason := ""
		if response.IncompleteDetails != nil {
			reason = response.IncompleteDetails.Reason
		}
		switch reason {
		case "max_output_tokens":
			finish = "length"
		case "content_filter":
			finish = "content_filter"
		default:
			return nil, native.Usage{}, fmt.Errorf("Responses incomplete reason %q cannot be converted", reason)
		}
	default:
		return nil, native.Usage{}, fmt.Errorf("Responses status %q cannot be converted", response.Status)
	}
	message := map[string]any{"role": "assistant", "content": nil}
	var text, refusal string
	var toolCalls []any
	for _, item := range response.Output {
		switch item.Type {
		case "reasoning":
			continue
		case "message":
			if item.Role != "" && item.Role != "assistant" {
				return nil, native.Usage{}, errors.New("Responses response role must be assistant")
			}
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					if len(part.Annotations) > 0 {
						return nil, native.Usage{}, errors.New("Responses output annotations cannot be converted")
					}
					text += part.Text
				case "refusal":
					refusal += part.Refusal
				default:
					return nil, native.Usage{}, fmt.Errorf("Responses content part %q cannot be converted", part.Type)
				}
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" {
				return nil, native.Usage{}, errors.New("invalid Responses function_call item")
			}
			toolCalls = append(toolCalls, map[string]any{"id": item.CallID, "type": "function",
				"function": map[string]string{"name": item.Name, "arguments": item.Arguments}})
		default:
			return nil, native.Usage{}, fmt.Errorf("Responses output item %q cannot be converted", item.Type)
		}
	}
	if text != "" {
		message["content"] = text
	}
	if refusal != "" {
		message["refusal"] = refusal
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		// Chat clients dispatch on the tool_calls finish reason.
		if finish == "stop" {
			finish = "tool_calls"
		}
	}
	usage := native.Usage{Complete: true}
	if response.Usage != nil {
		if response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 {
			return nil, native.Usage{}, errors.New("invalid Responses usage")
		}
		usage.Input, usage.Output, usage.Seen = response.Usage.InputTokens, response.Usage.OutputTokens, true
	}
	result := map[string]any{
		"id": response.ID, "object": "chat.completion", "model": requestedModel,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
	}
	if response.CreatedAt > 0 {
		result["created"] = response.CreatedAt
	} else {
		result["created"] = time.Now().Unix()
	}
	if usage.Seen {
		result["usage"] = map[string]int64{"prompt_tokens": usage.Input, "completion_tokens": usage.Output, "total_tokens": usage.Input + usage.Output}
	}
	converted, err := json.Marshal(result)
	return converted, usage, err
}
