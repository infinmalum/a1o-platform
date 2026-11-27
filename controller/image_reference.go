package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/billing"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/native"
)

const maxImageReferenceBytes = 5 << 20

var geminiImageRatios = map[string]string{
	"1:1": "1:1", "4:3": "4:3", "3:4": "3:4", "16:9": "16:9", "9:16": "9:16",
}

func isGeminiImageModel(name string) bool {
	switch strings.ToLower(name) {
	case "gemini-2.5-flash-image", "gemini-2.5-flash-image-preview",
		"gemini-3-pro-image-preview", "gemini-3-pro-image",
		"gemini-3.1-flash-image-preview", "gemini-3.1-flash-image", "gemini-3.1-flash-lite-image", "gemini-nano-banana-2.1":
		return true
	default:
		return false
	}
}

func geminiImageResolutionSupported(modelName, quality string) bool {
	if strings.HasPrefix(modelName, "gemini-2.5-") {
		return false
	}
	return quality == "1K" || quality == "2K" || quality == "4K"
}

func imageReferencePayload(userID, assetID, fileID int) ([]byte, error) {
	if assetID != 0 && fileID != 0 {
		return nil, errors.New("请选择一张参考图")
	}
	if assetID != 0 {
		asset, err := model.GetImageAsset(assetID, userID)
		if err != nil || asset.Size > maxImageReferenceBytes {
			return nil, errors.New("参考图不存在或超过 5 MiB")
		}
		return model.ReadImageAssetPayload(asset)
	}
	if fileID != 0 {
		file, err := model.GetChatFileById(fileID, userID)
		if err != nil || file.Kind != model.ChatFileKindImage || file.Size > maxImageReferenceBytes {
			return nil, errors.New("参考图不存在或超过 5 MiB")
		}
		return model.ReadChatFilePayload(file)
	}
	return nil, nil
}

func geminiImageRequest(gen *model.ImageGeneration) ([]byte, error) {
	parts := []map[string]any{{"text": gen.Prompt}}
	reference, err := imageReferencePayload(gen.UserId, gen.ReferenceAssetId, gen.ReferenceFileId)
	if err != nil {
		return nil, err
	}
	if gen.ReferenceAssetId != 0 || gen.ReferenceFileId != 0 {
		mime := http.DetectContentType(reference)
		switch mime {
		case "image/png", "image/jpeg", "image/webp":
			parts = append(parts, map[string]any{"inlineData": map[string]string{
				"mimeType": mime, "data": base64.StdEncoding.EncodeToString(reference),
			}})
		default:
			return nil, errors.New("参考图须为 PNG、JPEG 或 WebP")
		}
	}
	imageConfig := map[string]string{"aspectRatio": geminiImageRatios[gen.Size]}
	if gen.Quality != "" {
		imageConfig["imageSize"] = gen.Quality
	}
	return json.Marshal(map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"}, "imageConfig": imageConfig,
		},
	})
}

func geminiImageResults(raw []byte) ([]imageResultItem, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Thought    bool `json:"thought"`
					InlineData struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	var items []imageResultItem
	for _, candidate := range parsed.Candidates {
		for _, part := range candidate.Content.Parts {
			if !part.Thought && strings.HasPrefix(part.InlineData.MimeType, "image/") && part.InlineData.Data != "" {
				items = append(items, imageResultItem{B64: part.InlineData.Data})
			}
		}
	}
	return items, nil
}

func runGeminiImage(ctx context.Context, gen *model.ImageGeneration, principal lifecycle.Principal) (string, int, *lifecycle.HTTPError) {
	body, err := geminiImageRequest(gen)
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	var consumeLogID int
	recorder := &captureWriter{limit: base64.StdEncoding.EncodedLen(int(maxImageAssetBytes)) + (1 << 20)}
	herr := lifecycle.Forward(ctx, recorder, lifecycle.Request{
		Protocol: native.Gemini, Version: "v1beta", Action: "generateContent",
		Model: gen.Model, Body: body, Principal: principal, MaxOutputTokens: 4096, RetryLimit: config.RetryTimes,
		OnSettlement: func(settlement billing.Settlement) { consumeLogID = settlement.ConsumeLogID },
	})
	if herr != nil {
		return "", consumeLogID, herr
	}
	if recorder.status >= 400 {
		return "", consumeLogID, &lifecycle.HTTPError{Status: recorder.status, Message: recorder.body.String()}
	}
	items, err := geminiImageResults(recorder.body.Bytes())
	if err != nil {
		return "", consumeLogID, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "无法读取上游图像结果"}
	}
	result, err := json.Marshal(struct {
		Data []imageResultItem `json:"data"`
	}{items})
	if err != nil {
		return "", consumeLogID, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "无法保存上游图像结果"}
	}
	return string(result), consumeLogID, nil
}
