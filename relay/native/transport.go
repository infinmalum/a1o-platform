package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

type Protocol string

const (
	Anthropic         Protocol = "anthropic"
	Gemini            Protocol = "gemini"
	OpenAIChat        Protocol = "openai_chat"
	OpenAICompletions Protocol = "openai_completions"
	OpenAIEmbeddings  Protocol = "openai_embeddings"
	OpenAIModerations Protocol = "openai_moderations"
	OpenAIResponses   Protocol = "openai_responses"
	OpenAIImages      Protocol = "openai_images"
	OpenAIAudioSpeech Protocol = "openai_audio_speech"
	// Audio transcription and translation bodies are multipart forms that the
	// gateway forwards byte-for-byte; speech is a JSON request.
	OpenAIAudioTranscriptions Protocol = "openai_audio_transcriptions"
	OpenAIAudioTranslations   Protocol = "openai_audio_translations"
	// SystemOne is TypeSafe's decision-model wire format: a state plus typed
	// questions in, structured calibrated answers out, never streamed.
	SystemOne Protocol = "systemone"
)

// multipartAudio identifies the audio protocols whose Content-Type must come
// from the client request so the multipart boundary is preserved.
func (p Protocol) multipartAudio() bool {
	switch p {
	case OpenAIAudioTranscriptions, OpenAIAudioTranslations:
		return true
	}
	return false
}

// azureKeyAuth reports whether the protocol authenticates to Azure with the
// api-key header instead of a bearer token.
func (p Protocol) azureKeyAuth(channelType int) bool {
	if channelType != channeltype.Azure {
		return false
	}
	switch p {
	case OpenAIChat, OpenAICompletions, OpenAIEmbeddings, OpenAIModerations,
		OpenAIImages, OpenAIAudioSpeech, OpenAIAudioTranscriptions:
		return true
	}
	return false
}

type Request struct {
	Protocol     Protocol
	ChannelType  int
	BaseURL      string
	Version      string
	Model        string
	Action       string
	APIKey       string
	SystemPrompt string
	Body         []byte
	Headers      http.Header
	Query        url.Values
}

// BuildRequest changes only the fields required to reach the selected upstream.
// In particular, it never forwards the client's gateway token to the provider.
func BuildRequest(ctx context.Context, input Request) (*http.Request, error) {
	base, err := url.Parse(input.BaseURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.Fragment != "" || base.RawQuery != "" {
		return nil, errors.New("invalid upstream base URL")
	}
	if input.APIKey == "" {
		return nil, errors.New("upstream API key is empty")
	}
	var path string
	body := input.Body
	stream := false
	switch input.Protocol {
	case Anthropic:
		path = "/v1/messages"
		body, stream, err = prepareAnthropicBody(body, input.Model, input.SystemPrompt)
		if err != nil {
			return nil, err
		}
	case Gemini:
		if input.Model == "" || strings.ContainsAny(input.Model, "/?#:") {
			return nil, errors.New("invalid Gemini model name")
		}
		if input.Action != "generateContent" && input.Action != "streamGenerateContent" {
			return nil, errors.New("unsupported Gemini action")
		}
		version := input.Version
		if version == "" {
			version = "v1beta"
		}
		if version != "v1" && version != "v1beta" {
			return nil, errors.New("unsupported Gemini API version")
		}
		path = fmt.Sprintf("/%s/models/%s:%s", version, url.PathEscape(input.Model), input.Action)
		stream = input.Action == "streamGenerateContent"
		if input.SystemPrompt != "" {
			body, err = prepareGeminiBody(body, input.SystemPrompt)
			if err != nil {
				return nil, err
			}
		}
	case OpenAIResponses:
		path = "/v1/responses"
		body, stream, err = prepareOpenAIResponsesBody(body, input.Model, input.SystemPrompt)
		if err != nil {
			return nil, err
		}
	case OpenAIChat:
		path = "/v1/chat/completions"
		body, stream, err = prepareOpenAIChatBody(body, input.Model, input.SystemPrompt)
		if err != nil {
			return nil, err
		}
	case OpenAIEmbeddings:
		path = "/v1/embeddings"
		body, err = prepareOpenAIEmbeddingsBody(body, input.Model)
		if err != nil {
			return nil, err
		}
	case SystemOne:
		path = "/v1/systemone"
		body, err = prepareSystemOneBody(body, input.Model)
		if err != nil {
			return nil, err
		}
	case OpenAIModerations:
		path = "/v1/moderations"
		body, err = prepareOpenAIModerationsBody(body, input.Model)
		if err != nil {
			return nil, err
		}
	case OpenAICompletions:
		path = "/v1/completions"
		body, stream, err = prepareOpenAICompletionsBody(body, input.Model, input.SystemPrompt)
		if err != nil {
			return nil, err
		}
	case OpenAIImages:
		path = "/v1/images/generations"
		body, err = prepareOpenAIImagesBody(body, input.Model)
		if err != nil {
			return nil, err
		}
	case OpenAIAudioSpeech:
		path = "/v1/audio/speech"
		body, err = prepareOpenAIAudioSpeechBody(body, input.Model)
		if err != nil {
			return nil, err
		}
	case OpenAIAudioTranscriptions:
		path = "/v1/audio/transcriptions"
	case OpenAIAudioTranslations:
		path = "/v1/audio/translations"
	default:
		return nil, errors.New("unsupported native protocol")
	}
	// Azure deployments are addressed in the URL rather than the model field.
	switch input.Protocol {
	case OpenAIChat:
		if input.ChannelType == channeltype.Azure {
			if input.Model == "" || strings.ContainsAny(input.Model, "/?#") || input.Version == "" {
				return nil, errors.New("Azure Chat requires a mapped deployment and API version")
			}
			path = "/openai/deployments/" + url.PathEscape(strings.ReplaceAll(input.Model, ".", "")) + "/chat/completions"
		}
	case OpenAIImages, OpenAIAudioSpeech, OpenAIAudioTranscriptions, OpenAIAudioTranslations,
		OpenAICompletions, OpenAIEmbeddings, OpenAIModerations:
		if input.ChannelType == channeltype.Azure {
			if input.Model == "" || strings.ContainsAny(input.Model, "/?#") || input.Version == "" {
				return nil, errors.New("Azure requires a mapped deployment and API version")
			}
			deployment := url.PathEscape(strings.ReplaceAll(input.Model, ".", ""))
			switch input.Protocol {
			case OpenAIImages:
				path = "/openai/deployments/" + deployment + "/images/generations"
			case OpenAIAudioSpeech:
				path = "/openai/deployments/" + deployment + "/audio/speech"
			case OpenAIAudioTranscriptions:
				path = "/openai/deployments/" + deployment + "/audio/transcriptions"
			case OpenAICompletions:
				path = "/openai/deployments/" + deployment + "/completions"
			case OpenAIEmbeddings:
				path = "/openai/deployments/" + deployment + "/embeddings"
			case OpenAIModerations:
				path = "/openai/deployments/" + deployment + "/moderations"
			}
			break
		}
	}
	// OpenRouter's fixed base carries /api/v1 itself, and its unified API
	// serves the OpenAI family, Anthropic Messages, and SystemOne wires from
	// that one base as bare operation suffixes, so each canonical path drops
	// its version prefix outright.
	if input.ChannelType == channeltype.OpenRouter {
		switch input.Protocol {
		case OpenAIChat, OpenAIResponses, OpenAICompletions, OpenAIEmbeddings, OpenAIModerations,
			OpenAIImages, OpenAIAudioSpeech, OpenAIAudioTranscriptions, OpenAIAudioTranslations,
			Anthropic, SystemOne:
			path = strings.TrimPrefix(path, "/v1")
		}
	}
	// A vendor base URL on the OpenAI wire carries the version segment itself
	// (the filling rule mirrors relay/adaptor/openai.GetFullRequestURL), so the
	// path's /v1 is dropped to avoid doubling it; the official default base
	// keeps the versioned path. Azure is deployment-shaped and was handled
	// above; Anthropic, Gemini, and SystemOne keep the version in the path.
	if channeltype.IsValid(input.ChannelType) && input.ChannelType != channeltype.Azure &&
		input.BaseURL != channeltype.BaseURL(input.ChannelType) {
		switch input.Protocol {
		case OpenAIChat, OpenAIResponses, OpenAICompletions, OpenAIEmbeddings, OpenAIModerations,
			OpenAIImages, OpenAIAudioSpeech, OpenAIAudioTranscriptions, OpenAIAudioTranslations:
			path = strings.TrimPrefix(path, "/v1")
		}
	}
	endpoint := strings.TrimRight(base.String(), "/") + path
	if input.Protocol == OpenAIChat && strings.HasPrefix(base.Host, "gateway.ai.cloudflare.com") {
		switch input.ChannelType {
		case channeltype.OpenAI:
			endpoint = strings.TrimRight(base.String(), "/") + strings.TrimPrefix(path, "/v1")
		case channeltype.Azure:
			endpoint = strings.TrimRight(base.String(), "/") + strings.TrimPrefix(path, "/openai/deployments")
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	query := make(url.Values)
	for name, values := range input.Query {
		if strings.EqualFold(name, "key") || strings.EqualFold(name, "api_key") {
			continue
		}
		query[name] = append([]string(nil), values...)
	}
	if stream && input.Protocol == Gemini {
		query.Set("alt", "sse")
	}
	if input.ChannelType == channeltype.Azure && input.Version != "" {
		switch input.Protocol {
		case OpenAIChat, OpenAICompletions, OpenAIEmbeddings, OpenAIModerations, OpenAIImages,
			OpenAIAudioSpeech, OpenAIAudioTranscriptions, OpenAIAudioTranslations:
			query.Set("api-version", input.Version)
		}
	}
	req.URL.RawQuery = query.Encode()
	if input.Protocol.multipartAudio() {
		if contentType := input.Headers.Get("Content-Type"); contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	azureKey := input.Protocol.azureKeyAuth(input.ChannelType)
	switch input.Protocol {
	case Anthropic:
		if input.ChannelType == channeltype.OpenRouter {
			// OpenRouter authenticates every wire, Messages included, with
			// the same bearer key.
			req.Header.Set("Authorization", "Bearer "+input.APIKey)
		} else {
			req.Header.Set("x-api-key", input.APIKey)
		}
		version := input.Headers.Get("anthropic-version")
		if version == "" {
			version = "2023-06-01"
		}
		req.Header.Set("anthropic-version", version)
		for _, name := range []string{"anthropic-beta", "anthropic-workspace-id"} {
			if value := input.Headers.Get(name); value != "" {
				req.Header.Set(name, value)
			}
		}
	case Gemini:
		req.Header.Set("x-goog-api-key", input.APIKey)
	case SystemOne:
		req.Header.Set("Authorization", "Bearer "+input.APIKey)
	case OpenAIResponses, OpenAIChat, OpenAICompletions, OpenAIEmbeddings, OpenAIModerations,
		OpenAIImages, OpenAIAudioSpeech, OpenAIAudioTranscriptions, OpenAIAudioTranslations:
		if azureKey {
			req.Header.Set("api-key", input.APIKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+input.APIKey)
		}
		if beta := input.Headers.Get("OpenAI-Beta"); beta != "" {
			req.Header.Set("OpenAI-Beta", beta)
		}
	}
	return req, nil
}

func prepareOpenAIModerationsBody(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid OpenAI Moderations JSON request")
	}
	if len(fields["input"]) == 0 || string(fields["input"]) == "null" {
		return nil, errors.New("OpenAI Moderations input is required")
	}
	var requestedModel string
	if raw, ok := fields["model"]; ok {
		if err := json.Unmarshal(raw, &requestedModel); err != nil {
			return nil, errors.New("OpenAI Moderations model must be a string")
		}
	}
	if model == "" || model == requestedModel {
		return body, nil
	}
	fields["model"], _ = json.Marshal(model)
	return json.Marshal(fields)
}

func prepareOpenAICompletionsBody(body []byte, model, systemPrompt string) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, false, errors.New("invalid OpenAI Completions JSON request")
	}
	var requestedModel string
	if err := json.Unmarshal(fields["model"], &requestedModel); err != nil || requestedModel == "" {
		return nil, false, errors.New("OpenAI Completions model is required")
	}
	var prompt any
	if err := json.Unmarshal(fields["prompt"], &prompt); err != nil {
		return nil, false, errors.New("OpenAI Completions prompt is required")
	}
	if prompt == nil {
		return nil, false, errors.New("OpenAI Completions prompt is required")
	}
	var stream bool
	if raw, ok := fields["stream"]; ok && json.Unmarshal(raw, &stream) != nil {
		return nil, false, errors.New("OpenAI Completions stream must be boolean")
	}
	changed := false
	if model != "" && model != requestedModel {
		fields["model"], _ = json.Marshal(model)
		changed = true
	}
	// A configured system instruction has no dedicated field in the
	// Completions protocol, so it is prepended to every text prompt. Token
	// arrays cannot carry text and fail explicitly.
	if systemPrompt != "" {
		switch value := prompt.(type) {
		case string:
			fields["prompt"], _ = json.Marshal(systemPrompt + "\n" + value)
			changed = true
		case []any:
			texts := make([]any, 0, len(value))
			for _, item := range value {
				text, ok := item.(string)
				if !ok {
					return nil, false, errors.New("a configured system prompt cannot be applied to token prompts")
				}
				texts = append(texts, systemPrompt+"\n"+text)
			}
			fields["prompt"], _ = json.Marshal(texts)
			changed = true
		default:
			return nil, false, errors.New("a configured system prompt cannot be applied to token prompts")
		}
	}
	if !changed {
		return body, stream, nil
	}
	updated, err := json.Marshal(fields)
	return updated, stream, err
}

func prepareOpenAIEmbeddingsBody(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid OpenAI Embeddings JSON request")
	}
	var requestedModel string
	if err := json.Unmarshal(fields["model"], &requestedModel); err != nil || requestedModel == "" {
		return nil, errors.New("OpenAI Embeddings model is required")
	}
	if model == "" || model == requestedModel {
		return body, nil
	}
	fields["model"], _ = json.Marshal(model)
	return json.Marshal(fields)
}

// prepareSystemOneBody validates the shape the evaluation endpoint requires
// and rewrites the model name when a channel mapping applied. The state and
// questions payloads pass through untouched.
func prepareSystemOneBody(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid SystemOne JSON request")
	}
	if raw, ok := fields["state"]; !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("SystemOne state is required")
	}
	if raw, ok := fields["questions"]; !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("SystemOne questions is required")
	}
	var requestedModel string
	if err := json.Unmarshal(fields["model"], &requestedModel); err != nil || requestedModel == "" {
		return nil, errors.New("SystemOne model is required")
	}
	if model == "" || model == requestedModel {
		return body, nil
	}
	fields["model"], _ = json.Marshal(model)
	return json.Marshal(fields)
}

func prepareOpenAIChatBody(body []byte, model, systemPrompt string) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, false, errors.New("invalid OpenAI Chat JSON request")
	}
	var requestedModel string
	if err := json.Unmarshal(fields["model"], &requestedModel); err != nil || requestedModel == "" {
		return nil, false, errors.New("OpenAI Chat model is required")
	}
	var stream bool
	if raw, ok := fields["stream"]; ok {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return nil, false, errors.New("OpenAI Chat stream must be boolean")
		}
	}
	if (model == "" || model == requestedModel) && systemPrompt == "" {
		return body, stream, nil
	}
	if model != "" && model != requestedModel {
		fields["model"], _ = json.Marshal(model)
	}
	if systemPrompt != "" {
		var messages []json.RawMessage
		if err := json.Unmarshal(fields["messages"], &messages); err != nil || messages == nil {
			return nil, false, errors.New("OpenAI Chat messages must be an array")
		}
		instruction, _ := json.Marshal(map[string]string{"role": "system", "content": systemPrompt})
		if len(messages) > 0 {
			var first map[string]json.RawMessage
			if json.Unmarshal(messages[0], &first) == nil {
				var role string
				if json.Unmarshal(first["role"], &role) == nil && role == "system" {
					first["content"], _ = json.Marshal(systemPrompt)
					messages[0], _ = json.Marshal(first)
				} else {
					messages = append([]json.RawMessage{instruction}, messages...)
				}
			} else {
				messages = append([]json.RawMessage{instruction}, messages...)
			}
		} else {
			messages = append(messages, instruction)
		}
		fields["messages"], _ = json.Marshal(messages)
	}
	updated, err := json.Marshal(fields)
	return updated, stream, err
}

func prepareAnthropicBody(body []byte, model, systemPrompt string) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, false, errors.New("invalid Anthropic JSON request")
	}
	var requestedModel string
	if err := json.Unmarshal(fields["model"], &requestedModel); err != nil || requestedModel == "" {
		return nil, false, errors.New("Anthropic model is required")
	}
	var stream bool
	if raw, ok := fields["stream"]; ok {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return nil, false, errors.New("Anthropic stream must be boolean")
		}
	}
	if (model == "" || model == requestedModel) && systemPrompt == "" {
		return body, stream, nil
	}
	if model != "" && model != requestedModel {
		encoded, err := json.Marshal(model)
		if err != nil {
			return nil, false, err
		}
		fields["model"] = encoded
	}
	if systemPrompt != "" {
		encoded, err := json.Marshal(systemPrompt)
		if err != nil {
			return nil, false, err
		}
		fields["system"] = encoded
	}
	body, err := json.Marshal(fields)
	return body, stream, err
}

func prepareGeminiBody(body []byte, systemPrompt string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid Gemini JSON request")
	}
	instruction, err := json.Marshal(map[string]any{"parts": []map[string]string{{"text": systemPrompt}}})
	if err != nil {
		return nil, err
	}
	fields["systemInstruction"] = instruction
	return json.Marshal(fields)
}

// prepareOpenAIImagesBody replaces the model and preserves unknown fields on
// the Images wire format.
func prepareOpenAIImagesBody(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid OpenAI Images JSON request")
	}
	if model == "" {
		return body, nil
	}
	var requestedModel string
	if raw, ok := fields["model"]; ok {
		if err := json.Unmarshal(raw, &requestedModel); err != nil {
			return nil, errors.New("OpenAI Images model must be a string")
		}
	}
	if model == requestedModel {
		return body, nil
	}
	fields["model"], _ = json.Marshal(model)
	return json.Marshal(fields)
}

// prepareOpenAIAudioSpeechBody replaces the model on the JSON speech request.
func prepareOpenAIAudioSpeechBody(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid OpenAI Audio Speech JSON request")
	}
	if model == "" {
		return body, nil
	}
	var requestedModel string
	if raw, ok := fields["model"]; ok {
		if err := json.Unmarshal(raw, &requestedModel); err != nil {
			return nil, errors.New("OpenAI Audio Speech model must be a string")
		}
	}
	if model == requestedModel {
		return body, nil
	}
	fields["model"], _ = json.Marshal(model)
	return json.Marshal(fields)
}

func prepareOpenAIResponsesBody(body []byte, model, instructions string) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, false, errors.New("invalid OpenAI Responses JSON request")
	}
	var requestedModel string
	if err := json.Unmarshal(fields["model"], &requestedModel); err != nil || requestedModel == "" {
		return nil, false, errors.New("OpenAI Responses model is required")
	}
	var stream bool
	if raw, ok := fields["stream"]; ok {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return nil, false, errors.New("OpenAI Responses stream must be boolean")
		}
	}
	if (model == "" || model == requestedModel) && instructions == "" {
		return body, stream, nil
	}
	if model != "" && model != requestedModel {
		encoded, err := json.Marshal(model)
		if err != nil {
			return nil, false, err
		}
		fields["model"] = encoded
	}
	if instructions != "" {
		encoded, err := json.Marshal(instructions)
		if err != nil {
			return nil, false, err
		}
		fields["instructions"] = encoded
	}
	updated, err := json.Marshal(fields)
	return updated, stream, err
}
