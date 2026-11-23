package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
)

var arkImageSizes = []string{"2048x2048", "2816x1584", "1584x2816"}
var arkVideoRatios = []string{"16:9", "9:16"}

const arkVideoResolution = "720p"
const arkMaxVideoBytes = 64 << 20
const arkVideoTasks = "/contents/generations/tasks"
const arkMediaResponseLimit = 1 << 20

var studioMediaDownloadClient = client.NewPublicContentClient()

var arkTaskID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func arkImageName(name string) bool { return strings.HasPrefix(name, "doubao-seedream-") }
func arkVideoName(name string) bool { return strings.HasPrefix(name, "doubao-seedance-") }

func selectArkMedia(name, group string, quantity int) (*model.Channel, int64, error) {
	channel, selectionErr := lifecycle.SelectChannel(lifecycle.Selection{
		Group: group, Model: name, Type: channeltype.VolcengineArk, Source: model.ChannelSourceAPIOnly,
	})
	if selectionErr != nil {
		return nil, 0, errors.New(selectionErr.Message)
	}
	var prices map[string]int64
	raw := os.Getenv("ARK_MEDIA_PRICES")
	if len(raw) > 32768 || json.Unmarshal([]byte(raw), &prices) != nil || prices[name] <= 0 {
		return nil, 0, errors.New("Ark model requires a positive per-unit quota in ARK_MEDIA_PRICES")
	}
	total := math.Ceil(float64(prices[name]) * float64(quantity) * billingratio.GetGroupRatio(group))
	if total < 1 || total >= 1_000_000_000_000 || math.IsNaN(total) {
		return nil, 0, errors.New("invalid Ark model quota")
	}
	return channel, int64(total), nil
}

func validateArkMedia(req imageCreateRequest) bool {
	if req.N != 1 || len(req.Prompt) > 4096 {
		return false
	}
	if arkVideoName(req.Model) {
		return req.VideoDuration >= 5 && req.VideoDuration <= 8 && req.Quality == arkVideoResolution &&
			slices.Contains(arkVideoRatios, req.Size)
	}
	return req.VideoDuration == 0 && req.Quality == "" && slices.Contains(arkImageSizes, req.Size)
}

func arkReference(gen *model.ImageGeneration) (string, error) {
	payload, err := imageReferencePayload(gen.UserId, gen.ReferenceAssetId, gen.ReferenceFileId)
	if err != nil || len(payload) == 0 {
		return "", err
	}
	mime := http.DetectContentType(payload)
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		return "", errors.New("invalid Ark reference MIME type")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(payload), nil
}

type arkMediaResult struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"`
	Data    []imageResultItem `json:"data"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
}

func arkMediaRequest(ctx context.Context, channel *model.Channel, method, path string, data map[string]any) (arkMediaResult, int, error) {
	var result arkMediaResult
	var body io.Reader
	if data != nil {
		payload, err := json.Marshal(data)
		if err != nil {
			return result, 0, err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, channeltype.BaseURL(channeltype.VolcengineArk)+path, body)
	if err != nil {
		return result, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+channel.Key)
	req.Header.Set("Content-Type", "application/json")
	var transport http.RoundTripper
	if client.HTTPClient != nil {
		transport = client.HTTPClient.Transport
	}
	upstream := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := upstream.Do(req)
	if err != nil {
		return result, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, resp.StatusCode, fmt.Errorf("Ark returned HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, arkMediaResponseLimit+1))
	if err != nil {
		return result, resp.StatusCode, err
	}
	if len(payload) > arkMediaResponseLimit {
		return result, resp.StatusCode, errors.New("Ark response exceeds limit")
	}
	return result, resp.StatusCode, json.Unmarshal(payload, &result)
}

func startArkMedia(ctx context.Context, gen *model.ImageGeneration) error {
	group, err := model.CacheGetUserGroup(gen.UserId)
	if err != nil {
		return err
	}
	channel, quota, err := selectArkMedia(gen.Model, group, max(1, gen.VideoDuration))
	if err != nil {
		return err
	}
	ref, err := arkReference(gen)
	if err != nil {
		return err
	}
	upstreamModel := channel.GetModelMapping()[gen.Model]
	if upstreamModel == "" {
		upstreamModel = gen.Model
	}
	path := "/images/generations"
	var body map[string]any
	if gen.VideoDuration > 0 {
		if !arkVideoName(gen.Model) {
			return errors.New("invalid Ark video model")
		}
		content := []map[string]any{{"type": "text", "text": gen.Prompt}}
		if ref != "" {
			content = append(content, map[string]any{"type": "image_url", "role": "first_frame", "image_url": map[string]string{"url": ref}})
		}
		path = arkVideoTasks
		body = map[string]any{"model": upstreamModel, "content": content, "duration": gen.VideoDuration,
			"resolution": gen.Quality, "ratio": gen.Size, "watermark": true}
	} else {
		if !arkImageName(gen.Model) || !slices.Contains(arkImageSizes, gen.Size) || gen.N != 1 {
			return errors.New("invalid Ark image settings")
		}
		body = map[string]any{"model": upstreamModel, "prompt": gen.Prompt, "size": gen.Size, "response_format": "url", "watermark": true}
		if ref != "" {
			body["image"] = ref
		}
	}
	if err := model.ReserveArkMedia(ctx, gen, channel.Id, quota); err != nil {
		return err
	}
	refreshArkQuota(gen)
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, status, err := arkMediaRequest(requestCtx, channel, http.MethodPost, path, body)
	if err != nil {
		if status >= 400 && status < 500 && status != http.StatusRequestTimeout {
			return finishArkMedia(gen, false, "Ark rejected the generation request")
		}
		return arkMediaUncertain(gen, err)
	}
	if gen.VideoDuration > 0 {
		if !arkTaskID.MatchString(result.ID) {
			return arkMediaUncertain(gen, errors.New("Ark did not return a valid task ID"))
		}
		if err := model.StoreArkVideoTask(gen, result.ID); err != nil {
			return arkMediaUncertain(gen, err)
		}
		return nil
	}
	// Save the provider result before consuming the reservation. Retrying a
	// failed save can reuse this response without a second generation.
	if len(result.Data) != 1 || result.Data[0].URL == "" {
		return arkMediaUncertain(gen, errors.New("Ark did not return one image URL"))
	}
	if err := persistImageProducts(gen, result.Data); err != nil {
		raw, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return arkMediaUncertain(gen, marshalErr)
		}
		saved := string(raw)
		return settleArkMedia(gen, model.ImageGenStatusSaveFailed, err.Error(), &saved)
	}
	return finishArkMedia(gen, true, "")
}

func pollArkVideo(ctx context.Context, gen *model.ImageGeneration) {
	if err := checkArkVideo(ctx, gen); err != nil {
		logger.Error(ctx, fmt.Sprintf("Ark video %d: %v", gen.Id, err))
		if time.Since(time.Unix(gen.CreatedTime, 0)) > 48*time.Hour {
			_ = arkMediaUncertain(gen, err)
			return
		}
		if err := model.ScheduleArkVideoPoll(gen, 30*time.Second); err != nil {
			logger.Error(ctx, "reschedule Ark video: "+err.Error())
		}
	}
}

func checkArkVideo(ctx context.Context, gen *model.ImageGeneration) error {
	slots, err := model.GetRunImageAssetSlots(gen.Id, gen.UserId)
	if err != nil {
		return err
	}
	if slots[0] {
		return finishArkMedia(gen, true, "")
	}
	channel, err := model.GetChannelById(gen.ArkChannelID, true)
	if err != nil || channel.Type != channeltype.VolcengineArk {
		return errors.New("original Ark channel unavailable")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	result, _, err := arkMediaRequest(queryCtx, channel, http.MethodGet, arkVideoTasks+"/"+url.PathEscape(gen.ArkTaskID), nil)
	cancel()
	if err != nil {
		return err
	}
	if result.ID != gen.ArkTaskID {
		return errors.New("Ark video response refers to a different task")
	}
	switch result.Status {
	case "queued", "running":
		return model.ScheduleArkVideoPoll(gen, 15*time.Second)
	case "failed", "expired", "cancelled":
		return finishArkMedia(gen, false, "Ark video generation "+result.Status)
	case "succeeded":
		if err := downloadArkVideo(ctx, gen, result.Content.VideoURL); err != nil {
			return err
		}
		return finishArkMedia(gen, true, "")
	default:
		return errors.New("unknown Ark video state")
	}
}

func downloadArkVideo(ctx context.Context, gen *model.ImageGeneration, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil || req.URL.Scheme != "https" {
		return errors.New("invalid Ark video URL")
	}
	response, err := studioMediaDownloadClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > arkMaxVideoBytes {
		return fmt.Errorf("Ark video download unavailable (HTTP %d, %d bytes)", response.StatusCode, response.ContentLength)
	}
	return model.SaveArkVideoAsset(gen, response.Body, arkMaxVideoBytes)
}

func finishArkMedia(gen *model.ImageGeneration, completed bool, message string) error {
	status := model.ImageGenStatusFailed
	if completed {
		status = model.ImageGenStatusCompleted
	}
	return settleArkMedia(gen, status, message, nil)
}

func settleArkMedia(gen *model.ImageGeneration, status, message string, results *string) error {
	ctx := context.Background()
	finished, err := model.FinishArkMedia(ctx, gen, status, message, results)
	if finished {
		refreshArkQuota(gen)
		if status != model.ImageGenStatusFailed {
			model.RecordConsumeLog(ctx, &model.Log{UserId: gen.UserId, ModelName: gen.Model, ChannelId: gen.ArkChannelID,
				Quota: int(gen.ArkQuota), TokenName: model.ChatTokenName, Content: "Studio Ark media"})
		}
	}
	return err
}

func refreshArkQuota(gen *model.ImageGeneration) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := model.CacheRefreshUserQuota(ctx, gen.UserId); err != nil {
		logger.Error(ctx, "refresh Ark media quota: "+err.Error())
	}
}

func arkMediaUncertain(gen *model.ImageGeneration, reason error) error {
	logger.SysError(fmt.Sprintf("Ark media %d needs reconciliation: %v", gen.Id, reason))
	return model.MarkArkMediaUncertain(gen.Id)
}
