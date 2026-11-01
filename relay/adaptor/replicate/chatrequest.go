package replicate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// ConvertChatRequest builds the prediction input from whichever client wire
// the request arrived on. Replicate language models take a single prompt plus
// sampling parameters, so each conversion is direct: the Chat wire reads the
// parsed TextRequest, and the Anthropic and Responses wires parse their raw
// body themselves — no wire is routed through another as an intermediate.
// The hosted stream endpoint only exists for streaming predictions.
func ConvertChatRequest(in *relaymodel.ConversionInput) (*ReplicateChatRequest, error) {
	request := &ReplicateChatRequest{Stream: true}
	var options struct {
		Input json.RawMessage `json:"replicate_input"`
	}
	if len(in.RawBody) > 0 && json.Unmarshal(in.RawBody, &options) == nil && len(options.Input) > 0 {
		var object map[string]json.RawMessage
		if json.Unmarshal(options.Input, &object) != nil || object == nil {
			return nil, errors.New("replicate_input must be an object")
		}
		// The native input replaces the whole conversation; a body that also
		// carries a conversation would be silently discarded, so mixing fails.
		if in.Request != nil && len(in.Request.Messages) > 0 {
			return nil, errors.New("replicate_input replaces the conversation body; send one or the other")
		}
		request.NativeInput = options.Input
		return request, nil
	}
	switch in.Inbound {
	case "", native.OpenAIChat:
		if in.Request == nil {
			return nil, errors.New("request is nil")
		}
		if len(in.Request.Tools) > 0 || in.Request.ToolChoice != nil {
			return nil, errors.New("Replicate prompt models cannot represent client tools")
		}
		var system, prompt strings.Builder
		for _, message := range in.Request.Messages {
			content, err := messageStringContent(message)
			if err != nil {
				return nil, err
			}
			if content == "" {
				return nil, errors.New("conversation message requires text content")
			}
			switch message.Role {
			case "system", "developer":
				system.WriteString(content)
				system.WriteString("\n")
			default:
				prompt.WriteString(message.Role)
				prompt.WriteString(": ")
				prompt.WriteString(content)
				prompt.WriteString("\n")
			}
		}
		if strings.TrimSpace(prompt.String()) == "" {
			return nil, errors.New("replicate conversion requires a conversation message")
		}
		request.Input.Prompt = prompt.String()
		request.Input.SystemPrompt = system.String()
		applyChatSampling(&request.Input, in.Request.Temperature, in.Request.TopP,
			in.Request.PresencePenalty, in.Request.FrequencyPenalty, in.Request.MaxTokens, chatStopSequences(in.Request.Stop))
	case native.Anthropic:
		return convertAnthropicChat(in.RawBody)
	case native.OpenAIResponses:
		return convertResponsesChat(in.RawBody)
	default:
		return nil, fmt.Errorf("client wire %q cannot be converted", in.Inbound)
	}
	return request, nil
}

func applyChatSampling(input *ChatInput, temperature, topP, presence, frequency *float64, maxTokens int, stop []string) {
	input.Temperature = 1.0
	input.TopP = 1.0
	if temperature != nil {
		input.Temperature = *temperature
	}
	if topP != nil {
		input.TopP = *topP
	}
	if presence != nil {
		input.PresencePenalty = *presence
	}
	if frequency != nil {
		input.FrequencyPenalty = *frequency
	}
	input.MaxTokens = 500
	if maxTokens > 0 {
		input.MaxTokens = maxTokens
	}
	if len(stop) > 0 {
		input.StopSequences = strings.Join(stop, ",")
	}
}

// chatStopSequences normalizes the Chat wire's stop field (string or array)
// into the sequence list the prediction input takes.
func chatStopSequences(raw any) []string {
	switch value := raw.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		return value
	case []any:
		var out []string
		for _, item := range value {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// convertAnthropicChat parses a Messages body onto the prediction input.
// Tool and thinking blocks fail loudly: the hosted text models accept a
// prompt, not a tool loop.
func convertAnthropicChat(body []byte) (*ReplicateChatRequest, error) {
	var fields struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		MaxTokens     int64    `json:"max_tokens"`
		Temperature   *float64 `json:"temperature"`
		TopP          *float64 `json:"top_p"`
		StopSequences []string `json:"stop_sequences"`
		Tools         []any    `json:"tools"`
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, errors.New("invalid Messages JSON request")
	}
	if len(fields.Tools) != 0 {
		return nil, errors.New("replicate language models do not accept client tools")
	}
	request := &ReplicateChatRequest{Stream: true}
	if text, err := rawTextOrBlocks(fields.System); err != nil {
		return nil, fmt.Errorf("system: %w", err)
	} else if text != "" {
		request.Input.SystemPrompt = text
	}
	var prompt strings.Builder
	for _, message := range fields.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, fmt.Errorf("Messages role %q cannot be converted", message.Role)
		}
		content, err := rawBlocksText(message.Content)
		if err != nil {
			return nil, err
		}
		prompt.WriteString(message.Role)
		prompt.WriteString(": ")
		prompt.WriteString(content)
		prompt.WriteString("\n")
	}
	if strings.TrimSpace(prompt.String()) == "" {
		return nil, errors.New("replicate conversion requires a conversation message")
	}
	request.Input.Prompt = prompt.String()
	applyChatSampling(&request.Input, fields.Temperature, fields.TopP, nil, nil,
		int(fields.MaxTokens), fields.StopSequences)
	return request, nil
}

// convertResponsesChat parses a Responses body onto the prediction input.
func convertResponsesChat(body []byte) (*ReplicateChatRequest, error) {
	var fields struct {
		Instructions    string   `json:"instructions"`
		MaxOutputTokens int64    `json:"max_output_tokens"`
		Temperature     *float64 `json:"temperature"`
		TopP            *float64 `json:"top_p"`
		Stream          bool     `json:"stream"`
		Tools           []any    `json:"tools"`
		Input           json.RawMessage
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, errors.New("invalid Responses JSON request")
	}
	if len(fields.Tools) > 0 {
		return nil, errors.New("Replicate prompt models cannot represent client tools")
	}
	request := &ReplicateChatRequest{Stream: true}
	request.Input.SystemPrompt = fields.Instructions
	var prompt strings.Builder
	var text string
	if json.Unmarshal(fields.Input, &text) == nil {
		prompt.WriteString("user: ")
		prompt.WriteString(text)
		prompt.WriteString("\n")
	} else {
		var items []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content json.RawMessage
		}
		if json.Unmarshal(fields.Input, &items) != nil || len(items) == 0 {
			return nil, errors.New("Responses input must be a string or a non-empty item array")
		}
		for _, item := range items {
			switch item.Type {
			case "reasoning":
				// Replicate prompt models expose no thinking capability, so
				// replayed reasoning history has no carrier here and is
				// skipped deliberately.
				continue
			case "", "message":
				role := item.Role
				if role == "" {
					role = "user"
				}
				content, err := responsesPartsText(item.Role, item.Content)
				if err != nil {
					return nil, err
				}
				prompt.WriteString(role)
				prompt.WriteString(": ")
				prompt.WriteString(content)
				prompt.WriteString("\n")
			default:
				return nil, fmt.Errorf("Responses input item %q cannot be converted", item.Type)
			}
		}
	}
	if strings.TrimSpace(prompt.String()) == "" {
		return nil, errors.New("replicate conversion requires conversation input")
	}
	request.Input.Prompt = prompt.String()
	applyChatSampling(&request.Input, fields.Temperature, fields.TopP, nil, nil,
		int(fields.MaxOutputTokens), nil)
	return request, nil
}

// responsesPartsText extracts the text of user input parts and assistant
// output parts, rejecting everything else.
func responsesPartsText(role string, raw json.RawMessage) (string, error) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", errors.New("Responses message content must be text or a part array")
	}
	var text strings.Builder
	for _, part := range parts {
		representable := (role == "assistant" && part.Type == "output_text") || (role != "assistant" && part.Type == "input_text")
		if !representable {
			return "", fmt.Errorf("replicate conversion cannot represent the %q content part", part.Type)
		}
		text.WriteString(part.Text)
	}
	return text.String(), nil
}

// messageStringContent extracts text-only chat content, rejecting parts
// Replicate cannot represent instead of silently dropping them.
func messageStringContent(message relaymodel.Message) (string, error) {
	if len(message.ToolCalls) > 0 || message.Role == "tool" {
		return "", errors.New("Replicate prompt models cannot represent tool messages")
	}
	switch content := message.Content.(type) {
	case string:
		return content, nil
	case []any:
		var text strings.Builder
		for _, item := range content {
			part, ok := item.(map[string]any)
			if !ok || part["type"] != "text" {
				return "", errors.New("Replicate prompt models require text content")
			}
			value, ok := part["text"].(string)
			if !ok {
				return "", errors.New("invalid text content")
			}
			text.WriteString(value)
		}
		return text.String(), nil
	default:
		return "", errors.New("Replicate prompt models require text content")
	}
}

// rawTextOrBlocks reads Anthropic system content, which is a string or text
// blocks. Malformed content fails instead of silently reducing to empty text.
func rawTextOrBlocks(raw json.RawMessage) (string, error) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil
	}
	return rawBlocksText(raw)
}

// rawBlocksText extracts text from a Messages content field, rejecting every
// non-text block kind.
func rawBlocksText(raw json.RawMessage) (string, error) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return "", errors.New("Messages content must be text or a block array")
	}
	var text strings.Builder
	for _, block := range blocks {
		var kind string
		_ = json.Unmarshal(block["type"], &kind)
		if len(block) != 2 || kind != "text" {
			return "", fmt.Errorf("replicate conversion cannot represent the %q content block", kind)
		}
		var value string
		if json.Unmarshal(block["text"], &value) != nil {
			return "", errors.New("text block text must be a string")
		}
		text.WriteString(value)
	}
	return text.String(), nil
}
