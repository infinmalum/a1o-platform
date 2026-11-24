package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/infinmalum/norma-e/llm"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/billing/ratio"
)

const generateImageToolName = "generate_image"

const generateImagePollInterval = time.Second

const generateImageWaitTimeout = 3 * time.Minute

func generateImageToolDef() Tool {
	return Tool{
		Name: generateImageToolName,
		Description: "生成图片。返回生成的图片本体（模型可直接查看）以及可在前端查看的资产链接。" +
			"prompt 应尽量具体描述画面内容与风格；一次最多生成 4 张，模型/尺寸不指定时自动选择。",
		Parameters: []byte(`{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "画面描述，使用英文通常效果更好"},
    "model": {"type": "string", "description": "可选，图像模型名"},
    "size": {"type": "string", "description": "可选，宽x高（如 1024x1024），需为所选模型支持的尺寸"},
    "n": {"type": "integer", "description": "可选，生成数量，默认 1，最大 4"}
  },
  "required": ["prompt"]
}`),
	}
}

func imageGenExec(userId int) toolFunc {
	return func(ctx context.Context, req toolRequest) toolResult {
		var args struct {
			Prompt string `json:"prompt"`
			Model  string `json:"model"`
			Size   string `json:"size"`
			N      int    `json:"n"`
		}
		if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
			return toolResult{Content: "参数解析失败: " + err.Error(), IsError: true}
		}
		if args.Prompt == "" {
			return toolResult{Content: "prompt 不能为空", IsError: true}
		}
		if args.N < 1 || args.N > 4 {
			return toolResult{Content: "n 必须在 1-4 之间", IsError: true}
		}
		return runGenerateImage(ctx, userId, args.Prompt, args.Model, args.Size, args.N)
	}
}

func runGenerateImage(ctx context.Context, userId int, prompt, modelName, size string, n int) toolResult {
	if modelName == "" {
		name, err := defaultImageModel(ctx, userId)
		if err != nil {
			return toolResult{Content: "选择图像模型失败: " + err.Error(), IsError: true}
		}
		modelName = name
	}
	if size == "" {
		size = defaultImageSize(modelName)
		if size == "" {
			return toolResult{Content: fmt.Sprintf("模型 %q 不支持图像生成", modelName), IsError: true}
		}
	} else if _, ok := ratio.ImageSizeRatios[modelName][size]; !ok {
		return toolResult{Content: fmt.Sprintf("模型 %q 不支持尺寸 %q", modelName, size), IsError: true}
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("agent:%d:%s:%s:%s:%d", userId, modelName, prompt, size, n)))
	generation := &model.ImageGeneration{
		UserId:         userId,
		Model:          modelName,
		Prompt:         prompt,
		Size:           size,
		N:              n,
		Status:         model.ImageGenStatusQueued,
		IdempotencyKey: "agent-" + hex.EncodeToString(digest[:16]),
	}
	// A matching idempotency key returns the prior run, so the model can poll
	// an identical in-flight or completed request instead of failing.
	gen, _, err := model.CreateImageGeneration(generation)
	if err != nil {
		return toolResult{Content: "创建生成任务失败: " + err.Error(), IsError: true}
	}
	deadline := time.Now().Add(generateImageWaitTimeout)
	for {
		latest, err := model.GetImageGeneration(gen.Id, userId)
		if err != nil {
			return toolResult{Content: "查询生成任务失败: " + err.Error(), IsError: true}
		}
		switch latest.Status {
		case model.ImageGenStatusCompleted:
			return imageGenResult(latest)
		case model.ImageGenStatusFailed, model.ImageGenStatusSaveFailed,
			model.ImageGenStatusCancelled, model.ImageGenStatusNeedsReconciliation:
			return toolResult{Content: fmt.Sprintf("图片生成未完成（%s）：%s", latest.Status, latest.Error), IsError: true}
		}
		if ctx.Err() != nil {
			return toolResult{Content: "图片生成被中断", IsError: true}
		}
		if time.Now().After(deadline) {
			return toolResult{Content: fmt.Sprintf(
				"图片仍在生成中，可稍后在图片工作台查看（任务 #%d）", generation.Id), IsError: true}
		}
		select {
		case <-ctx.Done():
			return toolResult{Content: "图片生成被中断", IsError: true}
		case <-time.After(generateImagePollInterval):
		}
	}
}

func imageGenResult(gen *model.ImageGeneration) toolResult {
	assets, err := model.GetRunImageAssets(gen.Id, gen.UserId)
	if err != nil {
		return toolResult{Content: "读取生成结果失败: " + err.Error(), IsError: true}
	}
	if len(assets) == 0 {
		return toolResult{Content: "生成完成但资产尚未落盘，可稍后在图片工作台查看", IsError: true}
	}
	var content strings.Builder
	content.WriteString(fmt.Sprintf("生成 %d 张图片（任务 #%d）：\n", len(assets), gen.Id))
	blocks := make([]llm.ContentBlock, 0, len(assets))
	for _, asset := range assets {
		fmt.Fprintf(&content, "- %s\n", assetURL(asset.Id))
		payload, err := model.ReadImageAssetPayload(asset)
		if err != nil {
			logger.SysError("generate_image: read asset payload: " + err.Error())
			continue
		}
		mediaType := imageMediaType(asset.Mime)
		if mediaType == "" {
			logger.SysError("generate_image: unsupported asset mime " + asset.Mime)
			continue
		}
		blocks = append(blocks, llm.ImageBlock(mediaType, base64.StdEncoding.EncodeToString(payload)))
	}
	return toolResult{Content: content.String(), Blocks: blocks}
}

func assetURL(assetId int) string {
	return "/api/studio/images/assets/" + strconv.Itoa(assetId) + "/content"
}

func imageMediaType(mime string) string {
	switch mime {
	case llm.MediaTypePNG, llm.MediaTypeJPEG, llm.MediaTypeGIF, llm.MediaTypeWEBP:
		return mime
	}
	return ""
}

func defaultImageModel(ctx context.Context, userId int) (string, error) {
	group, err := model.CacheGetUserGroup(userId)
	if err != nil {
		return "", err
	}
	names, err := model.CacheGetGroupModels(ctx, group)
	if err != nil {
		return "", err
	}
	fallback := ""
	for _, name := range names {
		if _, ok := ratio.ImageSizeRatios[name]; !ok {
			continue
		}
		if preferred := preferredImageModel(name); preferred {
			return name, nil
		}
		if fallback == "" {
			fallback = name
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("分组 %q 没有可用图像模型", group)
	}
	return fallback, nil
}

func preferredImageModel(name string) bool {
	_, hasOpenAI := ratio.ImageSizeRatios[name]["1024x1024"]
	return hasOpenAI && name != "dall-e-2"
}

func defaultImageSize(modelName string) string {
	sizes := ratio.ImageSizeRatios[modelName]
	if len(sizes) == 0 {
		return ""
	}
	if _, ok := sizes["1024x1024"]; ok {
		return "1024x1024"
	}
	keys := make([]string, 0, len(sizes))
	for size := range sizes {
		keys = append(keys, size)
	}
	sort.Strings(keys)
	return keys[0]
}
