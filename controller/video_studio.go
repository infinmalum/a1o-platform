package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/native"
)

const studioVideoModel = "gemini-omni-1.2-flash"
const studioVideoResolution = "360p"
const studioVideoTokensPerSecond = 1931
const maxStudioVideoBytes = 10 << 20

func validateStudioVideo(req imageCreateRequest) error {
	if req.Model != studioVideoModel || req.Quality != studioVideoResolution ||
		(req.Size != "16:9" && req.Size != "9:16") || req.N != 1 ||
		req.VideoDuration < 3 || req.VideoDuration > 8 || req.ReferenceAssetId != 0 || req.ReferenceFileId != 0 {
		return errors.New("请选择 360p、3 至 8 秒、横屏或竖屏的视频参数")
	}
	if len(req.Prompt) > 4096 {
		return errors.New("视频提示词超过 4096 字节")
	}
	return nil
}

func videoQuota(mappedModel, group string, seconds int) (int64, error) {
	if mappedModel != studioVideoModel {
		return 0, errors.New("视频渠道须使用支持的 Gemini Omni 模型")
	}
	price, err := billingratio.GetModelRatio(mappedModel)
	if err != nil {
		return 0, err
	}
	output, err := billingratio.GetCompletionRatio(mappedModel)
	if err != nil {
		return 0, err
	}
	quota := math.Ceil(price * output * billingratio.GetGroupRatio(group) * float64(seconds*studioVideoTokensPerSecond))
	if quota <= 0 || quota >= float64(math.MaxInt64) || math.IsNaN(quota) {
		return 0, errors.New("请先为该视频模型配置有效的视频输出价格")
	}
	return int64(quota), nil
}

func parseStudioVideo(raw []byte) (imageResultItem, error) {
	var result struct {
		Status string `json:"status"`
		Steps  []struct {
			Type    string `json:"type"`
			Content []struct {
				Type     string `json:"type"`
				MimeType string `json:"mime_type"`
				Data     string `json:"data"`
			} `json:"content"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return imageResultItem{}, err
	}
	if result.Status != "completed" {
		return imageResultItem{}, fmt.Errorf("上游视频生成未完成（%s）", result.Status)
	}
	for _, step := range result.Steps {
		if step.Type != "model_output" {
			continue
		}
		for _, part := range step.Content {
			if part.Type != "video" || part.MimeType != "video/mp4" || part.Data == "" {
				continue
			}
			if len(part.Data) > base64.StdEncoding.EncodedLen(maxStudioVideoBytes) {
				return imageResultItem{}, errors.New("视频文件超过 10 MiB")
			}
			if len(part.Data) < 16 {
				return imageResultItem{}, errors.New("上游返回的视频无效")
			}
			header, err := base64.StdEncoding.DecodeString(part.Data[:min(688, len(part.Data))])
			if err != nil || http.DetectContentType(header) != "video/mp4" {
				return imageResultItem{}, errors.New("上游返回的视频无效")
			}
			return imageResultItem{B64: part.Data}, nil
		}
	}
	return imageResultItem{}, errors.New("上游未返回 MP4 视频")
}

func runStudioVideo(ctx context.Context, gen *model.ImageGeneration, principal lifecycle.Principal) (string, int, *lifecycle.HTTPError) {
	channel, herr := lifecycle.SelectForRequest(lifecycle.Request{Model: gen.Model, Principal: principal}, native.GeminiInteractions)
	if herr != nil {
		return "", 0, herr
	}
	quota, err := videoQuota(mappedImageModel(channel, gen.Model), principal.Group, gen.VideoDuration)
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	body, err := json.Marshal(map[string]any{
		"model": gen.Model, "input": gen.Prompt, "store": false,
		"response_format": map[string]any{
			"type": "video", "duration": fmt.Sprintf("%ds", gen.VideoDuration),
			"aspect_ratio": gen.Size, "resolution": studioVideoResolution,
		},
	})
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	recorder := &captureWriter{limit: 64 << 10, discardSuccessful: true}
	var consumeLogID int
	var item imageResultItem
	var resultErr error
	herr = lifecycle.Forward(ctx, recorder, lifecycle.Request{
		Protocol: native.GeminiInteractions, Model: gen.Model, Body: body,
		Principal: principal, Channel: channel, FixedQuota: quota,
		MaxResponseBytes: int64(base64.StdEncoding.EncodedLen(maxStudioVideoBytes) + (1 << 20)),
		ResponseCharge: func(_ http.Header, payload []byte) (int64, int64, error) {
			item, resultErr = parseStudioVideo(payload)
			if resultErr != nil {
				// A success response that has no usable product is free.
				return 0, 0, nil
			}
			return quota, 0, nil
		},
		OnSettlement: func(logID int, _ native.Usage) { consumeLogID = logID },
	})
	if herr != nil {
		return "", consumeLogID, herr
	}
	if recorder.status >= 400 {
		return "", consumeLogID, &lifecycle.HTTPError{Status: recorder.status, Message: recorder.body.String()}
	}
	if resultErr != nil {
		return "", consumeLogID, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: resultErr.Error()}
	}
	result, err := json.Marshal(struct {
		Data []imageResultItem `json:"data"`
	}{[]imageResultItem{item}})
	if err != nil {
		return "", consumeLogID, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: err.Error()}
	}
	return string(result), consumeLogID, nil
}

func mappedImageModel(channel lifecycle.Channel, name string) string {
	if upstream := channel.ModelMapping[name]; upstream != "" {
		return upstream
	}
	return name
}
