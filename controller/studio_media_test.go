package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mediaTestTransport func(*http.Request) (*http.Response, error)

func (transport mediaTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestStudioMediaWorkerSavesGeminiEditsAndVideo(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.Log{}, &model.ImageGeneration{}, &model.ImageAsset{}, &model.ChatFile{}))
	oldDB, oldLogDB, oldClient := model.DB, model.LOG_DB, client.HTTPClient
	oldSQLite, oldRedis, oldCache, oldLog := common.UsingSQLite, common.RedisEnabled, config.MemoryCacheEnabled, config.LogConsumeEnabled
	oldInput, oldOutput := billingratio.ModelRatio2JSONString(), billingratio.CompletionRatio2JSONString()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, client.HTTPClient = oldDB, oldLogDB, oldClient
		common.UsingSQLite, common.RedisEnabled, config.MemoryCacheEnabled, config.LogConsumeEnabled = oldSQLite, oldRedis, oldCache, oldLog
		_ = billingratio.UpdateModelRatioByJSONString(oldInput)
		_ = billingratio.UpdateCompletionRatioByJSONString(oldOutput)
		_ = sqlDB.Close()
	})
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.RedisEnabled, config.MemoryCacheEnabled, config.LogConsumeEnabled = true, false, false, true
	require.NoError(t, billingratio.UpdateModelRatioByJSONString(`{"gemini-2.5-flash-image":0.5,"gemini-omni-1.2-flash":0.75}`))
	require.NoError(t, billingratio.UpdateCompletionRatioByJSONString(`{"gemini-2.5-flash-image":10,"gemini-omni-1.2-flash":8}`))
	user := model.User{Username: "media-owner", Group: "default", Status: model.UserStatusEnabled, Role: model.RoleCommonUser, Quota: 1000000}
	require.NoError(t, db.Create(&user).Error)
	base := "https://example.test"
	channel := model.Channel{Type: channeltype.Gemini, Key: "provider-key", Name: "media-test",
		Status: model.ChannelStatusEnabled, Group: "default", Models: "gemini-2.5-flash-image," + studioVideoModel, BaseURL: &base}
	require.NoError(t, channel.Insert())
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 16, 16))))
	sample, err := model.SaveChatFilePayload(user.Id, model.ChatFileKindImage, "reference.png", pngBytes.Bytes())
	require.NoError(t, err)
	_, err = imageReferencePayload(user.Id+1, 0, sample.Id)
	require.Error(t, err)

	mp4Header := append([]byte{0, 0, 0, 20}, []byte("ftypmp42\x00\x00\x00\x00mp42")...)
	require.Equal(t, "video/mp4", http.DetectContentType(mp4Header))
	upstreamCalls := 0
	client.HTTPClient = &http.Client{Transport: mediaTestTransport(func(req *http.Request) (*http.Response, error) {
		upstreamCalls++
		require.Equal(t, "provider-key", req.Header.Get("x-goog-api-key"))
		require.Empty(t, req.Header.Get("Authorization"))
		body, readErr := io.ReadAll(req.Body)
		require.NoError(t, readErr)
		var reply string
		switch req.URL.Path {
		case "/v1beta/models/gemini-2.5-flash-image:generateContent":
			require.Contains(t, string(body), `"mimeType":"image/png"`)
			require.Contains(t, string(body), base64.StdEncoding.EncodeToString(pngBytes.Bytes()))
			reply = `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` +
				base64.StdEncoding.EncodeToString(pngBytes.Bytes()) + `"}}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20}}`
		case "/v1beta/interactions":
			require.JSONEq(t, `{"model":"gemini-omni-1.2-flash","input":"animate","store":false,"response_format":{"type":"video","duration":"3s","aspect_ratio":"16:9","resolution":"360p"}}`, string(body))
			reply = `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","data":"` +
				base64.StdEncoding.EncodeToString(mp4Header) + `"}]}]}`
		default:
			t.Fatalf("unexpected upstream path %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(reply))}, nil
	})}
	failures := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:transient_image_completion", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if tx.Statement.Table == "image_generations" && ok && values["status"] == model.ImageGenStatusCompleted && failures == 0 {
			failures++
			tx.AddError(errors.New("temporary image completion write failure"))
		}
	}))
	ctx := context.Background()
	for _, tc := range []struct {
		gen  *model.ImageGeneration
		mime string
	}{
		{&model.ImageGeneration{UserId: user.Id, Model: "gemini-2.5-flash-image", Prompt: "edit",
			Size: "1:1", N: 1, ReferenceFileId: sample.Id, IdempotencyKey: "edit"}, "image/png"},
		{&model.ImageGeneration{UserId: user.Id, Model: studioVideoModel, Prompt: "animate",
			Size: "16:9", Quality: "360p", N: 1, VideoDuration: 3, IdempotencyKey: "video"}, "video/mp4"},
	} {
		_, created, err := model.CreateImageGeneration(tc.gen)
		require.NoError(t, err)
		require.True(t, created)
		claimed, err := model.ClaimQueuedImageGeneration()
		require.NoError(t, err)
		require.Equal(t, tc.gen.Id, claimed.Id)
		executeImageGeneration(ctx, claimed)
		stored, err := model.GetImageGeneration(claimed.Id, user.Id)
		require.NoError(t, err)
		require.Equal(t, model.ImageGenStatusCompleted, stored.Status, stored.Error)
		require.Nil(t, stored.Results)
		require.Positive(t, stored.ConsumeLogId)
		assets, err := model.GetRunImageAssets(stored.Id, user.Id)
		require.NoError(t, err)
		require.Len(t, assets, 1)
		require.Equal(t, tc.mime, assets[0].Mime)
		if tc.mime == "video/mp4" {
			runLog := model.Log{}
			require.NoError(t, db.First(&runLog, stored.ConsumeLogId).Error)
			require.EqualValues(t, 34758, runLog.Quota)
			engine := gin.New()
			engine.GET("/:id", func(c *gin.Context) {
				c.Set(ctxkey.Id, user.Id)
				ServeImageAsset(c)
			})
			request := httptest.NewRequest(http.MethodGet, "/"+strconv.Itoa(assets[0].Id), nil)
			request.Header.Set("Range", "bytes=0-7")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusPartialContent, response.Code)
			require.Equal(t, "bytes 0-7/20", response.Header().Get("Content-Range"))
			require.Equal(t, mp4Header[:8], response.Body.Bytes())
		}
	}
	require.Equal(t, 1, failures)
	require.Equal(t, 2, upstreamCalls, "retrying the completion write must not repeat generation")
	_, err = parseStudioVideo(json.RawMessage(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","data":"aGVsbG8="}]}]}`))
	require.Error(t, err)
}
