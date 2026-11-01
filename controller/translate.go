package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/adaptor/deepl"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// maxTranslationChars bounds one workbench translation. The first release
// translates one document per call; a batch contract (text[]) arrives with
// the UI that needs it.
const maxTranslationChars = 20000

// GetTranslationMeta reports which engines the caller can use: DeepL with
// the target languages its enabled channels expose, and the always-available
// LLM path with the caller's model list.
func GetTranslationMeta(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	languages, err := listDeepLLanguages()
	if err != nil {
		translateError(c, err)
		return
	}
	models, err := listUserModelNames(c.Request.Context(), userId)
	if err != nil {
		translateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"deepl_languages": languages,
		"models":          models,
	}})
}

// listDeepLLanguages derives the translatable target languages from every
// enabled DeepL channel's model list; entries must carry the bare-language
// or deepl- prefixed form.
func listDeepLLanguages() ([]string, error) {
	var channels []model.Channel
	if err := model.DB.Where("type = ? AND status = ?", channeltype.DeepL,
		model.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	languages := []string{}
	for _, channel := range channels {
		for _, name := range strings.Split(channel.Models, ",") {
			name = strings.TrimSpace(name)
			lang, ok := normalizeDeepLTargetLang(name)
			if ok && !seen[lang] {
				seen[lang] = true
				languages = append(languages, lang)
			}
		}
	}
	sort.Strings(languages)
	return languages, nil
}

// normalizeDeepLTargetLang maps a channel model name onto a DeepL target
// language code ("deepl-zh" / "zh" → "ZH", "en-us" → "EN-US"); anything else
// is not a translation entry.
func normalizeDeepLTargetLang(name string) (string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if upper == "DEEPL" || upper == "" {
		return "", false
	}
	if len(upper) > 6 && upper[:6] == "DEEPL-" {
		upper = upper[6:]
	}
	if len(upper) == 2 || (len(upper) == 5 && upper[2] == '-') {
		return upper, true
	}
	return "", false
}

func listUserModelNames(ctx context.Context, userId int) ([]string, error) {
	group, err := model.CacheGetUserGroup(userId)
	if err != nil {
		return nil, err
	}
	names, err := model.CacheGetGroupModels(ctx, group)
	if err != nil {
		return nil, err
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// translateRequest carries one workbench translation. Engine "deepl" needs
// an enabled DeepL channel; engine "llm" needs a model the caller can serve.
type translateRequest struct {
	Text           string `json:"text"`
	TargetLang     string `json:"target_lang"`
	SourceLang     string `json:"source_lang"`
	Engine         string `json:"engine"`
	Model          string `json:"model"`
	IdempotencyKey string `json:"idempotency_key"`
}

// CreateTranslation runs one billed translation: the row persists before the
// upstream call so a replayed click with the same key and content returns
// the stored result instead of paying twice.
func CreateTranslation(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var req translateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		translateError(c, fmt.Errorf("请求格式无效: %w", err))
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	req.TargetLang = strings.ToUpper(strings.TrimSpace(req.TargetLang))
	if req.Text == "" {
		translateError(c, errors.New("请输入要翻译的文本"))
		return
	}
	if utf8.RuneCountInString(req.Text) > maxTranslationChars {
		translateError(c, fmt.Errorf("单次翻译最多 %d 字符", maxTranslationChars))
		return
	}
	if req.TargetLang == "" {
		translateError(c, errors.New("请选择目标语言"))
		return
	}
	if req.Engine != model.TranslationEngineDeepL && req.Engine != model.TranslationEngineLLM {
		translateError(c, errors.New("engine 须为 deepl 或 llm"))
		return
	}
	if req.Engine == model.TranslationEngineLLM && strings.TrimSpace(req.Model) == "" {
		translateError(c, errors.New("LLM 翻译须选择模型"))
		return
	}
	record := &model.TranslationRecord{
		UserId: userId, Engine: req.Engine, Model: req.Model,
		SourceLang: strings.ToUpper(strings.TrimSpace(req.SourceLang)),
		TargetLang: req.TargetLang, InputText: req.Text,
		Status:         model.TranslationStatusRunning,
		IdempotencyKey: req.IdempotencyKey,
	}
	existing, created, err := model.CreateTranslationRecord(record)
	if err != nil {
		translateError(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": existing, "replayed": true})
		return
	}
	output, detected, billed, usage, consumeLogId, engineErr := runTranslation(c.Request.Context(), userId, record)
	if engineErr != nil {
		if failErr := model.FailTranslationRecord(record.Id, engineErr.Error()); failErr != nil {
			translateError(c, failErr)
			return
		}
		translateError(c, engineErr)
		return
	}
	if err := model.CompleteTranslationRecord(record.Id, output, detected, billed,
		int(usage.Input), int(usage.Output), consumeLogId); err != nil {
		translateError(c, err)
		return
	}
	stored, err := model.GetTranslationRecordById(record.Id, userId)
	if err != nil {
		translateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stored})
}

// runTranslation dispatches to the DeepL channel path or the LLM path and
// returns the output, detected language, billed characters, usage, and the
// consume log that settled the call.
func runTranslation(ctx context.Context, userId int, record *model.TranslationRecord) (
	output, detectedLang string, billedChars int, usage native.Usage, consumeLogId int, err error,
) {
	if record.Engine == model.TranslationEngineDeepL {
		return runDeepLTranslation(ctx, userId, record)
	}
	return runLLMTranslation(ctx, userId, record)
}

// runDeepLTranslation selects the caller's DeepL channel for the target
// language and calls /v2/translate under the shared lifecycle: reservation
// and settlement bill the authoritative billed_characters as input units on
// the "deepl-<lang>" model's contract price.
func runDeepLTranslation(ctx context.Context, userId int, record *model.TranslationRecord) (
	output string, detectedLang string, billedChars int, usage native.Usage, consumeLogId int, err error,
) {
	group, err := model.CacheGetUserGroup(userId)
	if err != nil {
		return "", "", 0, native.Usage{}, 0, fmt.Errorf("加载用户分组失败: %w", err)
	}
	billingModel := "deepl-" + strings.ToLower(record.TargetLang)
	rawChannel, herr := lifecycle.SelectChannel(lifecycle.Selection{
		Group: group, Model: billingModel, Types: []int{channeltype.DeepL},
		Source: lifecycle.InitialSource(),
	})
	if herr != nil {
		return "", "", 0, native.Usage{}, 0, errors.New(herr.Message)
	}
	channel, err := toLifecycleChannel(rawChannel)
	if err != nil {
		return "", "", 0, native.Usage{}, 0, err
	}
	token, err := model.EnsureChatToken(userId)
	if err != nil {
		return "", "", 0, native.Usage{}, 0, fmt.Errorf("计费令牌不可用: %w", err)
	}
	input := lifecycle.Request{
		Model:      billingModel,
		Principal:  lifecycle.Principal{UserID: userId, TokenID: token.Id, TokenName: token.Name},
		RetryLimit: config.RetryTimes,
		Channel:    channel,
		OnSettlement: func(logID int, settled native.Usage) {
			consumeLogId = logID
			usage = settled
		},
	}
	attempt := func(attemptChannel lifecycle.Channel, _ string) lifecycle.AdapterResult {
		result := lifecycle.AdapterResult{}
		payload, err := json.Marshal(deepl.Request{
			ShowBilledCharacters: true,
			Text:                 []string{record.InputText},
			TargetLang:           record.TargetLang,
		})
		if err != nil {
			result.Err = &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
			return result
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(attemptChannel.BaseURL, "/")+"/v2/translate", bytes.NewReader(payload))
		if err != nil {
			result.Err = &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
			return result
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "DeepL-Auth-Key "+attemptChannel.APIKey)
		response, err := client.HTTPClient.Do(request)
		if err != nil {
			result.Err = &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "上游请求失败: " + err.Error()}
			return result
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		if err != nil {
			result.Err = &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "读取上游响应失败: " + err.Error()}
			return result
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			result.Err = &lifecycle.HTTPError{Status: http.StatusBadGateway,
				Message: fmt.Sprintf("上游返回 %d: %s", response.StatusCode, truncateForError(body))}
			return result
		}
		var parsed deepl.Response
		if err := json.Unmarshal(body, &parsed); err != nil {
			result.Err = &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "上游响应格式无效"}
			return result
		}
		if parsed.Message != "" || len(parsed.Translations) != 1 {
			message := parsed.Message
			if message == "" {
				message = "上游返回的翻译数量无效"
			}
			result.Err = &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: message}
			return result
		}
		translation := parsed.Translations[0]
		billed := utf8.RuneCountInString(record.InputText)
		if translation.BilledCharacters != nil && *translation.BilledCharacters >= 0 {
			billed = *translation.BilledCharacters
		}
		// DeepL bills source characters: billed_characters is the
		// authoritative input usage; the translation itself is the output.
		result.Usage = native.Usage{Input: int64(billed),
			Output: int64(utf8.RuneCountInString(translation.Text)), Seen: true, Complete: true}
		detectedLang = translation.DetectedSourceLanguage
		output = translation.Text
		billedChars = billed
		return result
	}
	if herr := lifecycle.ForwardAdapter(ctx, input, attempt); herr != nil {
		return "", "", 0, native.Usage{}, 0, errors.New(herr.Message)
	}
	return output, detectedLang, billedChars, usage, consumeLogId, nil
}

// runLLMTranslation bills a normal token call and asks the model to output
// only the translation; DeepL's auto-detection has no LLM equivalent, so no
// detected language is claimed.
func runLLMTranslation(ctx context.Context, userId int, record *model.TranslationRecord) (
	string, string, int, native.Usage, int, error,
) {
	system := "你是专业翻译引擎。把用户给出的文本完整翻译为目标语言，只输出译文本身，" +
		"不输出任何解释、前缀或引号；保留原文的格式、代码块与专有名词。"
	result, err := callLLMOnce(ctx, userId, record.Model, system, record.InputText, 0, nil)
	if err != nil {
		return "", "", 0, native.Usage{}, 0, err
	}
	output := strings.TrimSpace(result.Content)
	if output == "" {
		return "", "", 0, native.Usage{}, 0, errors.New("模型返回了空译文")
	}
	return output, "", 0, native.Usage{Input: result.InputTokens, Output: result.OutputTokens, Seen: true, Complete: true},
		result.ConsumeLogId, nil
}

// GetTranslations lists the caller's recent translation history.
func GetTranslations(c *gin.Context) {
	records, err := model.GetUserTranslations(c.GetInt(ctxkey.Id), 50)
	if err != nil {
		translateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": records})
}

func translateError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
}
