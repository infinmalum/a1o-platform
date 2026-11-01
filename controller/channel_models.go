package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/model"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// upstreamModelClient connects only to public addresses: the fetch endpoint
// relays admin-supplied URLs, and a loopback or private upstream (a local
// Ollama, say) is refused here by design — model lists for such channels go
// through manual entry instead.
var upstreamModelClient = client.NewPublicContentClient()

// upstreamModelFetchTimeout bounds one vendor listing call.
const upstreamModelFetchTimeout = 10 * time.Second

type upstreamModelsRequest struct {
	Id      int    `json:"id"`
	Type    *int   `json:"type"`
	BaseURL string `json:"base_url"`
	Key     string `json:"key"`
}

// FetchChannelUpstreamModels resolves a vendor's live model list, either for
// a saved channel (id) or for the channel being composed in the edit form
// (type/base_url/key). The result feeds the model picker; manual entry stays
// available when a vendor cannot or may not be queried.
func FetchChannelUpstreamModels(c *gin.Context) {
	var req upstreamModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": friendlyBindError(err),
		})
		return
	}

	var channelType int
	var baseURL, key string
	if req.Id != 0 {
		channel, err := model.GetChannelById(req.Id, true)
		if err != nil || channel == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "渠道不存在",
			})
			return
		}
		channelType = channel.Type
		baseURL = channel.GetBaseURL()
		key = channel.Key
	} else {
		if req.Type == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "请先保存渠道或填写渠道类型",
			})
			return
		}
		channelType = *req.Type
		baseURL = req.BaseURL
		key = req.Key
	}

	if channelType == channeltype.OpenRouter {
		syncOpenRouterCatalog(c, baseURL, key)
		return
	}
	if channelType == channeltype.Vercel {
		syncVercelCatalog(c, baseURL, key)
		return
	}

	models, err := fetchUpstreamModels(upstreamModelClient, channelType, baseURL, key)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
}

// fetchUpstreamModels queries the vendor listing endpoint for the channel's
// wire protocol and returns sorted, de-duplicated model ids.
func fetchUpstreamModels(httpClient *http.Client, channelType int, baseURL, key string) ([]string, error) {

	if baseURL == "" {
		baseURL = channeltype.BaseURL(channelType)
	}
	if baseURL == "" {
		return nil, errors.New("请先填写代理地址")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("代理地址仅支持 http/https")
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	var listURL string
	switch channelType {
	case channeltype.Anthropic:
		listURL = baseURL + "/v1/models"
	case channeltype.Gemini:
		listURL = baseURL + "/v1beta/models"
	case channeltype.DeepL:
		// DeepL's listing endpoint returns the translatable target
		// languages; those codes are the channel's model names.
		listURL = baseURL + "/v2/languages?type=target"
	case channeltype.OpenRouter:
		// The fixed unified base carries /api/v1; the listing endpoint is
		// the bare suffix.
		listURL = baseURL + "/models"
	case channeltype.Vercel:
		// The gateway base carries /v1; the listing endpoint is the bare
		// suffix.
		listURL = baseURL + "/models"
	case channeltype.OpenAI, channeltype.OpenAIResponses:
		// A vendor base URL carries its own version segment (the same filling
		// rule as the request path); the official default keeps /v1 here.
		if baseURL != channeltype.BaseURL(channelType) {
			listURL = baseURL + "/models"
		} else {
			listURL = baseURL + "/v1/models"
		}
	default:
		listURL = baseURL + "/v1/models"
	}
	parsedURL, err := url.Parse(listURL)
	if err != nil {
		return nil, fmt.Errorf("代理地址无效：%s", err.Error())
	}
	req, err := http.NewRequest(http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("代理地址无效：%s", err.Error())
	}
	req.URL = parsedURL
	switch channelType {
	case channeltype.Anthropic:
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2026-10-21")
	case channeltype.Gemini:
		req.Header.Set("x-goog-api-key", key)
	case channeltype.DeepL:
		req.Header.Set("Authorization", "DeepL-Auth-Key "+key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}

	body, err := fetchUpstreamBody(httpClient, req)
	if err != nil {
		return nil, err
	}

	switch channelType {
	case channeltype.Gemini:
		return parseGeminiModels(body)
	case channeltype.TypeSafe:
		var list struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(list.Models))
		for _, m := range list.Models {
			if m.Name != "" {
				ids = append(ids, m.Name)
			}
		}
		sort.Strings(ids)
		return ids, nil
	case channeltype.DeepL:
		return parseDeepLLanguages(body)
	default:
		return parseOpenAIStyleModels(body)
	}
}

// fetchUpstreamBody performs one vendor listing call and returns the raw
// response body, rejecting any non-200 status.
func fetchUpstreamBody(httpClient *http.Client, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), upstreamModelFetchTimeout)
	defer cancel()
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("获取模型列表失败：%s", err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取模型列表失败：%s", err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取模型列表失败：上游返回 %d：%s", resp.StatusCode, truncateForError(body))
	}
	return body, nil
}

// pricedCatalogParser turns a vendor catalog body into the model ids plus
// their prices converted into ratio units.
type pricedCatalogParser func(body []byte) ([]string, map[string]billingratio.FetchedModelPrice, error)

// syncPricedUpstreamCatalog serves the fetch-models call for vendor kinds
// whose listing publishes per-token prices (OpenRouter, Vercel): it returns
// the catalog's model ids like any other vendor and, because the catalog
// prices the models, merges the prices into the price book for models that
// do not have one yet. The operator's own prices are never overwritten, so
// re-running a sync only prices newly introduced models; the response
// reports how many models were priced.
func syncPricedUpstreamCatalog(c *gin.Context, kind int, baseURL, key string, parse pricedCatalogParser) {
	if baseURL == "" {
		baseURL = channeltype.BaseURL(kind)
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		respondUpstreamModelError(c, errors.New("代理地址仅支持 http/https"))
		return
	}
	parsedURL, err := url.Parse(baseURL + "/models")
	if err != nil {
		respondUpstreamModelError(c, fmt.Errorf("代理地址无效：%s", err.Error()))
		return
	}
	req, err := http.NewRequest(http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		respondUpstreamModelError(c, fmt.Errorf("代理地址无效：%s", err.Error()))
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	body, err := fetchUpstreamBody(upstreamModelClient, req)
	if err != nil {
		respondUpstreamModelError(c, err)
		return
	}
	models, prices, err := parse(body)
	if err != nil {
		respondUpstreamModelError(c, err)
		return
	}
	synced := billingratio.MergeFetchedModelPrices(prices)
	if synced > 0 {
		if err := persistPriceBook(); err != nil {
			respondUpstreamModelError(c, fmt.Errorf("模型列表已获取，但价格同步写入失败：%s", err.Error()))
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "",
		"data":          models,
		"synced_prices": synced,
	})
}

func syncOpenRouterCatalog(c *gin.Context, baseURL, key string) {
	syncPricedUpstreamCatalog(c, channeltype.OpenRouter, baseURL, key, parseOpenRouterCatalog)
}

func syncVercelCatalog(c *gin.Context, baseURL, key string) {
	syncPricedUpstreamCatalog(c, channeltype.Vercel, baseURL, key, parseVercelCatalog)
}

func respondUpstreamModelError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": err.Error(),
	})
}

// persistPriceBook writes the merged price book to the options table so the
// console and other instances see it; without the write, SyncOptions would
// revert the in-memory merge at its next refresh.
func persistPriceBook() error {
	if err := model.UpdateOption("ModelRatio", billingratio.ModelRatio2JSONString()); err != nil {
		return err
	}
	return model.UpdateOption("CompletionRatio", billingratio.CompletionRatio2JSONString())
}

func truncateForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200] + "..."
	}
	return text
}

// openAIStyleModelList covers the OpenAI /models envelope shared by the
// OpenAI-compatible vendors and Anthropic (whose /v1/models returns the same
// shape).
type openAIStyleModelList struct {
	Data []struct {
		Id string `json:"id"`
	} `json:"data"`
}

type geminiModelList struct {
	Models []struct {
		Name                       string   `json:"name"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

// deepLLanguageList parses DeepL's /v2/languages array; each entry's language
// code doubles as a channel model name (the adaptor derives the target
// language from the model name).
type deepLLanguageList []struct {
	Language string `json:"language"`
}

func parseDeepLLanguages(body []byte) ([]string, error) {
	var list deepLLanguageList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("解析模型列表失败：%s", err.Error())
	}
	models := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, item := range list {
		if item.Language == "" || seen[item.Language] {
			continue
		}
		seen[item.Language] = true
		models = append(models, item.Language)
	}
	sort.Strings(models)
	return models, nil
}

func parseOpenAIStyleModels(body []byte) ([]string, error) {
	var list openAIStyleModelList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("解析模型列表失败：%s", err.Error())
	}
	models := make([]string, 0, len(list.Data))
	seen := make(map[string]bool, len(list.Data))
	for _, item := range list.Data {
		if item.Id == "" || seen[item.Id] {
			continue
		}
		seen[item.Id] = true
		models = append(models, item.Id)
	}
	sort.Strings(models)
	return models, nil
}

// openRouterModelList parses OpenRouter's public catalog. Each entry carries
// the live per-token pricing the platform bills with, so the same call that
// lists models also prices them.
type openRouterModelList struct {
	Data []struct {
		Id      string `json:"id"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

// parseOpenRouterCatalog returns the catalog's model ids plus per-model
// prices converted into ratio units: OpenRouter publishes USD per token, the
// ratio scale is $2 per million, so the input ratio is prompt × 500000 and
// the completion ratio is completion over prompt. Entries with malformed or
// negative prices, and models with free input but paid output (unexpressible
// in the charge formula, which multiplies the whole bill by the input ratio),
// are returned unpriced instead of guessed at.
func parseOpenRouterCatalog(body []byte) ([]string, map[string]billingratio.FetchedModelPrice, error) {
	var list openRouterModelList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, nil, fmt.Errorf("解析模型列表失败：%s", err.Error())
	}
	models := make([]string, 0, len(list.Data))
	prices := make(map[string]billingratio.FetchedModelPrice, len(list.Data))
	seen := make(map[string]bool, len(list.Data))
	for _, item := range list.Data {
		if item.Id == "" || seen[item.Id] {
			continue
		}
		seen[item.Id] = true
		models = append(models, item.Id)
		prompt, promptErr := strconv.ParseFloat(item.Pricing.Prompt, 64)
		completion, completionErr := strconv.ParseFloat(item.Pricing.Completion, 64)
		if promptErr != nil || completionErr != nil || prompt < 0 || completion < 0 {
			continue
		}
		var completionRatio float64
		switch {
		case prompt > 0:
			completionRatio = completion / prompt
		case completion == 0:
			// a free model bills nothing, which ratio zero expresses exactly
		default:
			continue
		}
		prices[item.Id] = billingratio.FetchedModelPrice{
			Input:      prompt * 500000,
			Completion: completionRatio,
		}
	}
	sort.Strings(models)
	return models, prices, nil
}

// vercelModelList parses the Vercel AI Gateway's /v1/models catalog. Every
// entry carries a `type` discriminator and per-token USD prices; only the
// language entries serve the OpenResponses wire the channel speaks, so the
// parser lists those alone instead of creating abilities the channel cannot
// answer (embeddings, image, speech, and the rest go through their own
// OpenAI-compatible channels).
type vercelModelList struct {
	Data []struct {
		Id      string `json:"id"`
		Type    string `json:"type"`
		Pricing struct {
			Input  string `json:"input"`
			Output string `json:"output"`
		} `json:"pricing"`
	} `json:"data"`
}

// parseVercelCatalog returns the catalog's language-model ids plus per-model
// prices converted into ratio units. The gateway publishes USD per token
// (spot-checked against vendor list prices), the ratio scale is $2 per
// million, so the input ratio is input × 500000 and the completion ratio is
// output over input. Entries with malformed or negative prices, and models
// with free input but paid output (unexpressible in the charge formula,
// which multiplies the whole bill by the input ratio), are returned unpriced
// instead of guessed at — the same rule the OpenRouter catalog applies.
func parseVercelCatalog(body []byte) ([]string, map[string]billingratio.FetchedModelPrice, error) {
	var list vercelModelList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, nil, fmt.Errorf("解析模型列表失败：%s", err.Error())
	}
	models := make([]string, 0, len(list.Data))
	prices := make(map[string]billingratio.FetchedModelPrice, len(list.Data))
	seen := make(map[string]bool, len(list.Data))
	for _, item := range list.Data {
		if item.Id == "" || seen[item.Id] {
			continue
		}
		seen[item.Id] = true
		if item.Type != "language" {
			continue
		}
		models = append(models, item.Id)
		prompt, promptErr := strconv.ParseFloat(item.Pricing.Input, 64)
		completion, completionErr := strconv.ParseFloat(item.Pricing.Output, 64)
		if promptErr != nil || completionErr != nil || prompt < 0 || completion < 0 {
			continue
		}
		var completionRatio float64
		switch {
		case prompt > 0:
			completionRatio = completion / prompt
		case completion == 0:
			// a free model bills nothing, which ratio zero expresses exactly
		default:
			continue
		}
		prices[item.Id] = billingratio.FetchedModelPrice{
			Input:      prompt * 500000,
			Completion: completionRatio,
		}
	}
	sort.Strings(models)
	return models, prices, nil
}

func parseGeminiModels(body []byte) ([]string, error) {
	var list geminiModelList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("解析模型列表失败：%s", err.Error())
	}
	models := make([]string, 0, len(list.Models))
	seen := make(map[string]bool, len(list.Models))
	for _, item := range list.Models {
		if !supportsGenerateContent(item.SupportedGenerationMethods) {
			continue
		}
		name := strings.TrimPrefix(item.Name, "models/")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, name)
	}
	sort.Strings(models)
	return models, nil
}

func supportsGenerateContent(methods []string) bool {
	if len(methods) == 0 {
		// Absent methods mean the model serves the standard generateContent
		// surface; only embed-only models list the restriction explicitly.
		return true
	}
	for _, method := range methods {
		if method == "generateContent" {
			return true
		}
	}
	return false
}
