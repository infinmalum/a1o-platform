package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// captureWriter collects a non-streaming lifecycle response so login-state
// services can drive billed calls without an HTTP client connection.
type captureWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *captureWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *captureWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

func (w *captureWriter) WriteHeader(status int) { w.status = status }

// llmCallResult reports one settled one-shot model call.
type llmCallResult struct {
	Content      string
	InputTokens  int64
	OutputTokens int64
	ConsumeLogId int
}

// callLLMOnce runs one non-streaming chat call under the user's login-state
// billing identity (the same internal chat token the chat engine uses). The
// channel is pinned after selection so a retry never crosses channels; the
// lifecycle owns reservation, settlement, and the consume log.
func callLLMOnce(ctx context.Context, userId int, modelName, system, user string, maxTokens int64, temperature *float64) (llmCallResult, error) {
	if modelName == "" {
		return llmCallResult{}, errors.New("未选择模型")
	}
	token, err := model.EnsureChatToken(userId)
	if err != nil {
		return llmCallResult{}, fmt.Errorf("计费令牌不可用: %w", err)
	}
	principal := lifecycle.Principal{UserID: userId, TokenID: token.Id, TokenName: token.Name}
	channel, herr := lifecycle.SelectForRequest(lifecycle.Request{
		Model: modelName, Principal: principal,
	}, native.OpenAIChat)
	if herr != nil {
		return llmCallResult{}, errors.New(herr.Message)
	}
	payload := map[string]any{
		"model": modelName,
		"messages": []map[string]any{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"stream": false,
	}
	if maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}
	if temperature != nil {
		payload["temperature"] = *temperature
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llmCallResult{}, err
	}
	recorder := &captureWriter{}
	result := llmCallResult{}
	herr = lifecycle.Forward(ctx, recorder, lifecycle.Request{
		Protocol: native.OpenAIChat, Model: modelName,
		Stream: false, Body: body,
		Channel:    channel,
		Principal:  lifecycle.Principal{UserID: userId, TokenID: token.Id, TokenName: token.Name, SpecificChannel: true, SpecificChannelID: channel.ID},
		RetryLimit: config.RetryTimes,
		OnSettlement: func(logID int, usage native.Usage) {
			result.ConsumeLogId = logID
			result.InputTokens = usage.Input
			result.OutputTokens = usage.Output
		},
	})
	if herr != nil {
		return llmCallResult{}, errors.New(herr.Message)
	}
	if recorder.status >= 400 {
		return llmCallResult{}, fmt.Errorf("模型调用失败（%d）: %s", recorder.status, recorder.body.String())
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(recorder.body.Bytes(), &completion); err != nil {
		return llmCallResult{}, fmt.Errorf("解析模型响应失败: %w", err)
	}
	if len(completion.Choices) == 0 {
		return llmCallResult{}, errors.New("模型未返回任何结果")
	}
	content, err := chatContentText(completion.Choices[0].Message.Content)
	if err != nil {
		return llmCallResult{}, err
	}
	result.Content = content
	return result, nil
}

// toLifecycleChannel converts a raw channel row into the lifecycle view the
// ForwardAdapter attempts consume, mirroring the internal channelFromModel:
// default base URL per channel kind, mapped models, and provider config.
func toLifecycleChannel(channel *model.Channel) (lifecycle.Channel, error) {
	if channel == nil {
		return lifecycle.Channel{}, errors.New("渠道不存在")
	}
	config, err := channel.LoadConfig()
	if err != nil {
		return lifecycle.Channel{}, err
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = channeltype.BaseURL(channel.Type)
	}
	selected := lifecycle.Channel{
		ID: channel.Id, Type: channel.Type, BaseURL: baseURL, APIKey: channel.Key,
		ModelMapping: channel.GetModelMapping(), APIVersion: config.APIVersion,
		Config: config,
	}
	if channel.UpstreamAccountID != nil {
		selected.UpstreamAccountID = *channel.UpstreamAccountID
	}
	return selected, nil
}

// chatContentText accepts both the plain string content and the content-part
// array shapes the chat wire allows.
func chatContentText(raw any) (string, error) {
	switch value := raw.(type) {
	case string:
		return value, nil
	case []any:
		text := ""
		for _, part := range value {
			object, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if piece, ok := object["text"].(string); ok {
				text += piece
			}
		}
		return text, nil
	case nil:
		return "", nil
	default:
		return "", errors.New("模型响应内容格式不受支持")
	}
}
