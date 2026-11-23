package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/stretchr/testify/require"
)

func TestArkVideoResumesSavedTaskAndStreamsAsset(t *testing.T) {
	t.Chdir(t.TempDir())
	db := setupChannelTypeTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.ImageGeneration{}, &model.ImageAsset{}, &model.ChatFile{}, &model.Log{}))
	user := model.User{Username: "ark-owner", Group: "default", Status: model.UserStatusEnabled, Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	const seedream = "doubao-seedream-5-5-261028"
	const seedance = "doubao-seedance-2-5-260628"
	t.Setenv("ARK_MEDIA_PRICES", `{"`+seedream+`":10,"`+seedance+`":20}`)
	channel := model.Channel{Type: channeltype.VolcengineArk, Status: model.ChannelStatusEnabled,
		Group: "default", Models: seedream + "," + seedance, Key: "ark-key", Name: "Ark"}
	require.NoError(t, channel.Insert())
	oldClient, oldDownload := client.HTTPClient, studioMediaDownloadClient
	t.Cleanup(func() { client.HTTPClient, studioMediaDownloadClient = oldClient, oldDownload })
	var ref bytes.Buffer
	require.NoError(t, png.Encode(&ref, image.NewRGBA(image.Rect(0, 0, 300, 300))))
	file, err := model.SaveChatFilePayload(user.Id, model.ChatFileKindImage, "ref.png", ref.Bytes())
	require.NoError(t, err)
	queries, submitted := 0, 0
	arkImageFails := true
	client.HTTPClient = &http.Client{Transport: mediaTestTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer ark-key", req.Header.Get("Authorization"))
		var body string
		status := 200
		switch req.URL.Path {
		case "/api/v3/images/generations":
			payload, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(payload), `"size":"2048x2048"`)
			require.Contains(t, string(payload), `"image":"data:image/png;base64,`)
			body = `{"data":[{"url":"https://assets.example/image.png"}]}`
			if arkImageFails {
				body, status = `{"error":{"message":"upstream rejected"}}`, 400
			}
		case "/api/v3/contents/generations/tasks":
			require.Equal(t, http.MethodPost, req.Method)
			payload, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(payload), `"role":"first_frame"`)
			require.Contains(t, string(payload), `"duration":5`)
			require.Contains(t, string(payload), `"resolution":"720p"`)
			submitted++
			body = `{"id":"cgt-test"}`
		case "/api/v3/contents/generations/tasks/cgt-test":
			require.Equal(t, http.MethodGet, req.Method)
			queries++
			body = `{"id":"cgt-test","status":"succeeded","content":{"video_url":"https://assets.example/clip.mp4"}}`
		default:
			t.Fatalf("unexpected Ark endpoint %s", req.URL)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	mp4 := append([]byte{0, 0, 0, 20}, []byte("ftypmp42\x00\x00\x00\x00mp42")...)
	studioMediaDownloadClient = &http.Client{Transport: mediaTestTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "assets.example", req.URL.Host)
		require.Empty(t, req.Header.Get("Authorization"))
		if req.URL.Path == "/image.png" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(ref.Bytes()))}, nil
		}
		require.Equal(t, "/clip.mp4", req.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(mp4))}, nil
	})}

	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(ctxkey.Id, user.Id) })
	engine.GET("/meta", GetImageMeta)
	engine.POST("/generate", CreateImageGeneration)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/meta", nil))
	require.Contains(t, rec.Body.String(), `"ark_image_models":["`+seedream+`"]`)
	require.Contains(t, rec.Body.String(), `"ark_video_models":["`+seedance+`"]`)
	ctx := context.Background()
	submit := func(payload imageCreateRequest) *model.ImageGeneration {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(rec, req)
		require.Contains(t, rec.Body.String(), `"success":true`)
		gen, err := model.ClaimQueuedImageGeneration()
		require.NoError(t, err)
		require.NotNil(t, gen)
		executeImageGeneration(ctx, gen)
		return gen
	}
	imageJob := submit(imageCreateRequest{Model: seedream, ReferenceFileId: file.Id, Prompt: "edit", IdempotencyKey: "ark-image"})
	stored, err := model.GetImageGeneration(imageJob.Id, user.Id)
	require.NoError(t, err)
	require.Equal(t, model.ImageGenStatusFailed, stored.Status)
	quota, err := model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 1000, quota)
	arkImageFails = false
	imageJob = submit(imageCreateRequest{Model: seedream, ReferenceFileId: file.Id, Prompt: "edit", IdempotencyKey: "ark-image-success"})
	stored, err = model.GetImageGeneration(imageJob.Id, user.Id)
	require.NoError(t, err)
	require.Equal(t, model.ImageGenStatusCompleted, stored.Status)
	images, err := model.GetRunImageAssets(imageJob.Id, user.Id)
	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, "image/png", images[0].Mime)

	videoJob := submit(imageCreateRequest{Model: seedance, ReferenceFileId: file.Id, Prompt: "animate", IdempotencyKey: "ark-video",
		VideoDuration: 5, Size: "9:16", Quality: "720p"})
	stored, err = model.GetImageGeneration(videoJob.Id, user.Id)
	require.NoError(t, err)
	require.Equal(t, "cgt-test", stored.ArkTaskID)
	require.Equal(t, model.ImageGenStatusRunning, stored.Status)
	quota, err = model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 890, quota)

	moved, err := model.ReconcileStuckImageGenerations()
	require.NoError(t, err)
	require.Zero(t, moved)
	poll, err := model.ClaimArkVideoPoll()
	require.NoError(t, err)
	require.NotNil(t, poll)
	second, err := model.ClaimArkVideoPoll()
	require.NoError(t, err)
	require.Nil(t, second)
	pollArkVideo(ctx, poll)
	stored, err = model.GetImageGeneration(videoJob.Id, user.Id)
	require.NoError(t, err)
	require.Equal(t, model.ImageGenStatusCompleted, stored.Status)
	require.Zero(t, stored.ArkQuota)
	require.Equal(t, 1, submitted)
	require.Equal(t, 1, queries)
	assets, err := model.GetRunImageAssets(videoJob.Id, user.Id)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	data, err := model.ReadImageAssetPayload(assets[0])
	require.NoError(t, err)
	require.Equal(t, mp4, data)
	var usage int64
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Select("used_quota").Scan(&usage).Error)
	require.EqualValues(t, 110, usage)
}
