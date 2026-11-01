package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	relaycontroller "github.com/infinmalum/a1o-platform/relay/controller"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// Image workbench (studio proposal §7). Creation is a queued database row; a
// same-process worker claims and executes it, so leaving the page never
// cancels a generation. Upstream calls reuse the /v1 image billing: the
// model-and-size ratio times the image count, reserved then charged exactly.
const (
	imageMaxActivePerUser = 5
	imageWorkerCount      = 2
	imageWorkerPollEvery  = 2 * time.Second
	maxImageAssetBytes    = int64(25 << 20)
)

// GetImageMeta backs the creation form: the caller's servable model list
// with the models the pricing table knows marked, their size options, and
// the concurrency cap. Unknown-capability models stay selectable — the
// gateway validates parameters at execution and reports precise failures.
func GetImageMeta(c *gin.Context) {
	models, err := listUserModelNames(c.Request.Context(), c.GetInt(ctxkey.Id))
	if err != nil {
		imageError(c, err)
		return
	}
	known := make([]string, 0, len(models))
	other := make([]string, 0, len(models))
	for _, name := range models {
		if _, ok := billingratio.ImageSizeRatios[name]; ok {
			known = append(known, name)
		} else {
			other = append(other, name)
		}
	}
	sizes := map[string][]string{}
	for name, table := range billingratio.ImageSizeRatios {
		options := make([]string, 0, len(table))
		for size := range table {
			options = append(options, size)
		}
		sort.Strings(options)
		sizes[name] = options
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"known_models": known,
		"other_models": other,
		"known_sizes":  sizes,
		"max_active":   imageMaxActivePerUser,
	}})
}

type imageCreateRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	Size           string `json:"size"`
	Quality        string `json:"quality"`
	N              int    `json:"n"`
	IdempotencyKey string `json:"idempotency_key"`
}

// CreateImageGeneration validates the request against the shared image
// pricing tables and queues it. Nothing upstream happens here: the worker
// owns the billed call, so a dropped connection cannot cancel a generation.
func CreateImageGeneration(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var req imageCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		imageError(c, fmt.Errorf("请求格式无效: %w", err))
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		imageError(c, errors.New("请选择图像模型"))
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		imageError(c, errors.New("请输入提示词"))
		return
	}
	if req.N == 0 {
		req.N = 1
	}
	if req.Size == "" {
		req.Size = "1024x1024"
	}
	mapped := req.Model
	if err := validateImageGeneration(req.Model, req.Prompt, req.Size, req.Quality, req.N); err != nil {
		imageError(c, err)
		return
	}
	active, err := model.CountActiveImageGenerations(userId)
	if err != nil {
		imageError(c, err)
		return
	}
	if active >= imageMaxActivePerUser {
		imageError(c, fmt.Errorf("最多同时进行 %d 个图像生成，请等待完成后再试", imageMaxActivePerUser))
		return
	}
	gen := &model.ImageGeneration{
		UserId: userId, Model: mapped, Prompt: req.Prompt,
		Size: req.Size, Quality: req.Quality, N: req.N,
		IdempotencyKey: req.IdempotencyKey,
	}
	existing, created, err := model.CreateImageGeneration(gen)
	if err != nil {
		imageError(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": existing, "replayed": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gen})
}

func parsePathId(c *gin.Context) (int, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return 0, errors.New("无效的 ID")
	}
	return id, nil
}

// validateImageGeneration mirrors the /v1 images validation so the workbench
// never sends parameters the pricing tables reject, and refuses unpriced
// models instead of reserving a zero charge.
func validateImageGeneration(modelName, prompt, size, quality string, n int) error {
	if _, err := billingratio.GetModelRatio(modelName); err != nil {
		return err
	}
	if !imageSizeSupported(modelName, size) {
		return fmt.Errorf("模型 %s 不支持尺寸 %s", modelName, size)
	}
	if !imagePromptLengthSupported(modelName, len(prompt)) {
		return errors.New("提示词超过该模型的长度上限")
	}
	if !imageCountSupported(modelName, n) {
		return fmt.Errorf("模型 %s 不支持生成 %d 张", modelName, n)
	}
	if quality != "" && quality != "standard" && quality != "hd" {
		return errors.New("quality 须为 standard 或 hd")
	}
	return nil
}

// imageGenerationView decorates a row with its saved products.
type imageGenerationView struct {
	*model.ImageGeneration
	Assets []*model.ImageAsset `json:"assets"`
}

func GetImageGenerationStatus(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := parsePathId(c)
	if err != nil {
		imageError(c, err)
		return
	}
	gen, err := model.GetImageGeneration(id, userId)
	if err != nil {
		imageError(c, errors.New("图像生成不存在"))
		return
	}
	assets, err := model.GetRunImageAssets(gen.Id, userId)
	if err != nil {
		imageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": imageGenerationView{gen, assets}})
}

// ListImageGenerations returns the recent runs and the gallery's most recent
// products in one call.
func ListImageGenerations(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	rows, err := model.GetUserImageGenerations(userId, 50)
	if err != nil {
		imageError(c, err)
		return
	}
	assets, err := model.GetUserImageAssets(userId, 60)
	if err != nil {
		imageError(c, err)
		return
	}
	views := make([]imageGenerationView, 0, len(rows))
	for _, row := range rows {
		views = append(views, imageGenerationView{row, nil})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"generations": views,
		"assets":      assets,
	}})
}

// RetryImageSave re-attempts persisting the upstream products of a
// save_failed generation. The generation itself is never re-run: the upstream
// call was already charged, and Results carries its output.
func RetryImageSave(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := parsePathId(c)
	if err != nil {
		imageError(c, err)
		return
	}
	gen, err := model.GetImageGeneration(id, userId)
	if err != nil {
		imageError(c, errors.New("图像生成不存在"))
		return
	}
	if gen.Status != model.ImageGenStatusSaveFailed {
		imageError(c, errors.New("仅保存失败的生成可以重试保存"))
		return
	}
	if gen.Results == nil || *gen.Results == "" {
		imageError(c, errors.New("没有可保存的上游结果"))
		return
	}
	items, parseErr := parseImageResults(*gen.Results)
	if parseErr != nil {
		imageError(c, parseErr)
		return
	}
	if err := persistImageProducts(gen, items); err != nil {
		_ = model.MarkImageGenerationSaveFailed(gen.Id, *gen.Results, err.Error(), gen.ConsumeLogId)
		imageError(c, err)
		return
	}
	if err := model.CompleteImageGeneration(gen.Id, *gen.Results, gen.ConsumeLogId); err != nil {
		imageError(c, err)
		return
	}
	// Reload so the response carries the terminal status, not the stale row.
	stored, err := model.GetImageGeneration(gen.Id, userId)
	if err != nil {
		imageError(c, err)
		return
	}
	assets, _ := model.GetRunImageAssets(gen.Id, userId)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": imageGenerationView{stored, assets}})
}

// CancelImageGeneration cancels a queued generation; a running one cannot be
// withdrawn from the upstream.
func CancelImageGeneration(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := parsePathId(c)
	if err != nil {
		imageError(c, err)
		return
	}
	if err := model.CancelQueuedImageGeneration(id, userId); err != nil {
		imageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ServeImageAsset streams one owned product for <img> display.
func ServeImageAsset(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := parsePathId(c)
	if err != nil {
		imageError(c, err)
		return
	}
	asset, err := model.GetImageAsset(id, userId)
	if err != nil {
		imageError(c, errors.New("素材不存在"))
		return
	}
	payload, err := model.ReadImageAssetPayload(asset)
	if err != nil {
		imageError(c, errors.New("素材文件已丢失"))
		return
	}
	c.Data(http.StatusOK, asset.Mime, payload)
}

// ---- worker ----

// StartImageWorker reconciles rows a crashed process left running (the
// upstream may have been billed, so they become needs_reconciliation rather
// than retried) and starts the claim workers.
func StartImageWorker() {
	if count, err := model.ReconcileStuckImageGenerations(); err != nil {
		logger.SysError("image generation reconciliation failed: " + err.Error())
	} else if count > 0 {
		logger.SysLog(fmt.Sprintf("image generation reconciliation: %d running rows moved to needs_reconciliation", count))
	}
	for i := 0; i < imageWorkerCount; i++ {
		go imageWorkerLoop()
	}
}

func imageWorkerLoop() {
	for {
		gen, err := model.ClaimQueuedImageGeneration()
		if err != nil {
			logger.SysError("image worker claim failed: " + err.Error())
			time.Sleep(imageWorkerPollEvery)
			continue
		}
		if gen == nil {
			time.Sleep(imageWorkerPollEvery)
			continue
		}
		executeImageGeneration(context.Background(), gen)
	}
}

// executeImageGeneration runs one claimed generation to a terminal state.
// Every exit marks the row; nothing upstream is retried blindly.
func executeImageGeneration(ctx context.Context, gen *model.ImageGeneration) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.SysError(fmt.Sprintf("image worker panic on generation %d: %v", gen.Id, recovered))
			_ = model.FailImageGeneration(gen.Id, "图像生成执行异常，请重试")
		}
	}()
	responseJSON, consumeLogId, herr := runImageUpstream(ctx, gen)
	if herr != nil {
		_ = model.FailImageGeneration(gen.Id, herr.Message)
		return
	}
	items, err := parseImageResults(responseJSON)
	if err != nil {
		_ = model.FailImageGeneration(gen.Id, err.Error())
		return
	}
	if len(items) == 0 {
		_ = model.FailImageGeneration(gen.Id, "上游未返回图像")
		return
	}
	if err := persistImageProducts(gen, items); err != nil {
		// The upstream succeeded and was charged; only the save may be
		// retried, never a regeneration.
		_ = model.MarkImageGenerationSaveFailed(gen.Id, responseJSON, err.Error(), consumeLogId)
		return
	}
	_ = model.CompleteImageGeneration(gen.Id, responseJSON, consumeLogId)
}

// runImageUpstream prefers OpenAI-wire image channels and falls back to the
// retained provider format (Replicate). Both settle through the shared
// lifecycle with the exact operation quota.
func runImageUpstream(ctx context.Context, gen *model.ImageGeneration) (string, int, *lifecycle.HTTPError) {
	group, err := model.CacheGetUserGroup(gen.UserId)
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: "加载用户分组失败"}
	}
	token, err := model.EnsureChatToken(gen.UserId)
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: "计费令牌不可用"}
	}
	principal := lifecycle.Principal{UserID: gen.UserId, TokenID: token.Id, TokenName: token.Name, Group: group}

	payload := map[string]any{
		"model": gen.Model, "prompt": gen.Prompt,
		"n": gen.N, "size": gen.Size,
	}
	if gen.Quality != "" {
		payload["quality"] = gen.Quality
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
	}

	var consumeLogId int
	onSettlement := func(logID int, _ native.Usage) { consumeLogId = logID }

	nativeChannel, herr := lifecycle.SelectForRequest(lifecycle.Request{
		Model: gen.Model, Principal: principal,
	}, native.OpenAIImages)
	if herr == nil {
		recorder := &captureWriter{}
		herr = lifecycle.Forward(ctx, recorder, lifecycle.Request{
			Protocol: native.OpenAIImages, Model: gen.Model,
			Stream: false, Body: body, FixedQuota: imageGenerationQuota(gen),
			Channel:      nativeChannel,
			Principal:    lifecycle.Principal{UserID: gen.UserId, TokenID: token.Id, TokenName: token.Name, Group: group, SpecificChannel: true, SpecificChannelID: nativeChannel.ID},
			RetryLimit:   config.RetryTimes,
			OnSettlement: onSettlement,
		})
		if herr == nil {
			// The fixed quota settled inside Forward: this attempt is final
			// whether the body parses or not — never a second billed call.
			if recorder.status >= 400 {
				return "", consumeLogId, &lifecycle.HTTPError{Status: recorder.status, Message: recorder.body.String()}
			}
			return recorder.body.String(), consumeLogId, nil
		}
		// A failed attempt refunded its reserve inside Forward, so the
		// provider-format fallback may proceed.
	}
	replicateChannel, selectErr := lifecycle.SelectChannel(lifecycle.Selection{
		Group: group, Model: gen.Model, Types: []int{channeltype.Replicate},
		Source: lifecycle.InitialSource(),
	})
	if selectErr != nil {
		if herr != nil {
			return "", 0, herr
		}
		return "", 0, selectErr
	}
	channel, err := toLifecycleChannel(replicateChannel)
	if err != nil {
		return "", 0, &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
	}
	imageRequest := &relaymodel.ImageRequest{
		Model: gen.Model, Prompt: gen.Prompt, N: gen.N,
		Size: gen.Size, Quality: gen.Quality,
	}
	recorder := &captureWriter{}
	herr = relaycontroller.GenerateProviderImages(ctx, imageRequest,
		lifecycle.Principal{UserID: gen.UserId, TokenID: token.Id, TokenName: token.Name, Group: group},
		channel, imageGenerationQuota(gen), recorder)
	if herr != nil {
		return "", 0, herr
	}
	return recorder.body.String(), consumeLogId, nil
}

// imageGenerationQuota computes the fixed operation charge with the same
// formula the /v1 images relay uses: model and group ratio times the
// model/size cost times the image count.
func imageGenerationQuota(gen *model.ImageGeneration) int64 {
	modelRatio, err := billingratio.GetModelRatio(gen.Model)
	if err != nil {
		return 0
	}
	group, err := model.CacheGetUserGroup(gen.UserId)
	if err != nil {
		return 0
	}
	groupRatio := billingratio.GetGroupRatio(group)
	ratio := modelRatio * groupRatio
	quota := int64(ratio*imageCostRatio(gen.Model, gen.Size, gen.Quality)*1000) * int64(gen.N)
	return quota
}

// imageResultItem is one entry of the upstream data array.
type imageResultItem struct {
	URL string `json:"url"`
	B64 string `json:"b64_json"`
}

func parseImageResults(raw string) ([]imageResultItem, error) {
	var parsed struct {
		Data []imageResultItem `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("解析上游结果失败: %w", err)
	}
	return parsed.Data, nil
}

// persistImageProducts saves every not-yet-persisted product: base64 decodes
// in place, URLs are fetched server-side through the public-content client
// (public addresses only, scheme and redirect checked, size capped). A
// slot that already has an asset is skipped so a retry-save never duplicates.
func persistImageProducts(gen *model.ImageGeneration, items []imageResultItem) error {
	saved, err := model.GetRunImageAssetSlots(gen.Id, gen.UserId)
	if err != nil {
		return err
	}
	var failures []string
	for index, item := range items {
		if saved[index] {
			continue
		}
		payload, err := loadImageProductPayload(item)
		if err != nil {
			failures = append(failures, fmt.Sprintf("第 %d 张: %s", index+1, err.Error()))
			continue
		}
		if _, err := model.SaveImageAssetPayload(gen.UserId, gen.Id, index, payload); err != nil {
			failures = append(failures, fmt.Sprintf("第 %d 张: %s", index+1, err.Error()))
		}
	}
	if len(failures) > 0 {
		return errors.New("保存失败: " + strings.Join(failures, "；"))
	}
	return nil
}

// loadImageProductPayload materializes one product's bytes from base64 or a
// URL. URL fetches are capped and restricted to public hosts — the same
// guard the user-content path applies, never weakened for product URLs.
func loadImageProductPayload(item imageResultItem) ([]byte, error) {
	switch {
	case item.B64 != "":
		payload, err := base64.StdEncoding.DecodeString(item.B64)
		if err != nil {
			return nil, fmt.Errorf("base64 解码失败: %w", err)
		}
		if int64(len(payload)) > maxImageAssetBytes {
			return nil, errors.New("图像超过大小上限")
		}
		return payload, nil
	case item.URL != "":
		fetcher := client.NewPublicContentClient()
		request, err := http.NewRequest(http.MethodGet, item.URL, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("图像地址无效: %w", err)
		}
		response, err := fetcher.Do(request)
		if err != nil {
			return nil, fmt.Errorf("图像下载失败: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("图像下载返回 %d（链接可能已过期）", response.StatusCode)
		}
		contentType := response.Header.Get("Content-Type")
		if contentType != "" && !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("下载内容不是图像（%s）", contentType)
		}
		payload, err := io.ReadAll(io.LimitReader(response.Body, maxImageAssetBytes+1))
		if err != nil {
			return nil, fmt.Errorf("图像下载失败: %w", err)
		}
		if int64(len(payload)) > maxImageAssetBytes {
			return nil, errors.New("图像超过大小上限")
		}
		if len(payload) == 0 {
			return nil, errors.New("下载内容为空")
		}
		return payload, nil
	default:
		return nil, errors.New("上游结果既没有 url 也没有 b64_json")
	}
}

func imageError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
}
