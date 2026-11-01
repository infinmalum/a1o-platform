package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/common/helper"
	"github.com/infinmalum/a1o-platform/common/random"
	"github.com/infinmalum/a1o-platform/monitor"
	"github.com/infinmalum/a1o-platform/relay"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/controller/validator"
	"github.com/infinmalum/a1o-platform/relay/ginmeta"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/meta"
	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/native"
	"github.com/infinmalum/a1o-platform/relay/relaymode"
)

// The provider controllers retain the provider-specific wire adapters for
// the channel types whose upstream wire has no native protocol (Coze, DeepL,
// Replicate) while the shared lifecycle owns channel retries and quota
// settlement. The adapters run against the framework-neutral adaptor.Context
// built here, so no provider route depends on the legacy Gin relay controller.

// parseTextRequest parses an OpenAI wire body for the provider converters.
func parseTextRequest(body []byte, relayMode int, modelParam string) (*relaymodel.TextRequest, error) {
	textRequest := &relaymodel.TextRequest{}
	if err := json.Unmarshal(body, textRequest); err != nil {
		return nil, err
	}
	if relayMode == relaymode.Moderations && textRequest.Model == "" {
		textRequest.Model = native.DefaultModerationModel
	}
	if relayMode == relaymode.Embeddings && textRequest.Model == "" {
		textRequest.Model = modelParam
	}
	if err := validator.ValidateTextRequest(textRequest, relayMode); err != nil {
		return nil, err
	}
	return textRequest, nil
}

// providerTransport builds the framework-neutral adapter context. Request
// metadata the adapters read (the request ID) is seeded here; channel and
// converter values are refreshed per attempt.
func providerTransport(c *gin.Context) *adaptor.Context {
	transport := adaptor.NewContext(c.Request, c.Writer)
	transport.Set(helper.RequestIdKey, c.GetString(helper.RequestIdKey))
	return transport
}

// providerChannel derives the lifecycle channel view from the dispatcher.
func providerChannel(metadata *meta.Meta) lifecycle.Channel {
	return lifecycle.Channel{
		ID: metadata.ChannelId, Type: metadata.ChannelType,
		BaseURL: metadata.BaseURL, APIKey: metadata.APIKey,
		ModelMapping: metadata.ModelMapping, SystemPrompt: metadata.ForcedSystemPrompt,
		APIVersion: metadata.Config.APIVersion, Config: metadata.Config,
		UpstreamAccountID: metadata.UpstreamAccountID,
	}
}

// providerMeta builds the per-attempt metadata from the selected channel.
func providerMeta(channel lifecycle.Channel, originModel, mappedModel string,
	mode int, isStream bool, promptTokens int, requestURLPath string, principal lifecycle.Principal,
) *meta.Meta {
	return &meta.Meta{
		Mode: mode, ChannelType: channel.Type, ChannelId: channel.ID,
		TokenId: principal.TokenID, TokenName: principal.TokenName,
		UserId: principal.UserID, Group: principal.Group,
		ModelMapping:    channel.ModelMapping,
		OriginModelName: originModel, ActualModelName: mappedModel,
		BaseURL: channel.BaseURL, APIKey: channel.APIKey,
		APIType: channeltype.ToAPIType(channel.Type), Config: channel.Config,
		IsStream: isStream, PromptTokens: promptTokens,
		ForcedSystemPrompt: channel.SystemPrompt,
		RequestURLPath:     requestURLPath, StartTime: time.Now(),
	}
}

// monitorProviderAttempt reports each provider attempt to the channel health
// monitor the same way the legacy retry loop did. Upstream errors may disable
// the channel; a served response marks it healthy.
func monitorProviderAttempt(channel lifecycle.Channel, err *lifecycle.HTTPError, upstream *lifecycle.UpstreamError) {
	if err == nil {
		monitor.Emit(channel.ID, true)
		return
	}
	go func(status int, detail *relaymodel.Error) {
		if monitor.ShouldDisableChannel(detail, status) && detail != nil {
			monitor.DisableChannel(channel.ID, "", detail.Message)
		} else {
			monitor.Emit(channel.ID, false)
		}
	}(err.Status, upstreamDetail(upstream, err))
}

func upstreamDetail(upstream *lifecycle.UpstreamError, err *lifecycle.HTTPError) *relaymodel.Error {
	if upstream != nil {
		return &relaymodel.Error{Message: upstream.Message, Type: upstream.Type, Code: upstream.Code, Param: upstream.Param}
	}
	return &relaymodel.Error{Message: err.Message}
}

// providerTextAttempt runs one provider adapter attempt for a text-shaped
// operation (chat, messages, responses): convert, send, and forward the
// response through the adapter's own handlers. inbound names the client wire
// the body arrived on; the Chat wire parses into TextRequest, the Anthropic
// and Responses wires pass through as RawBody for the adapter's own native
// conversion.
func providerTextAttempt(body []byte, relayMode int, modelParam string, inbound native.Protocol, principal lifecycle.Principal, transport *adaptor.Context) lifecycle.AdapterAttempt {
	return func(channel lifecycle.Channel, mappedModel string) lifecycle.AdapterResult {
		request, err := parseProviderRequest(body, relayMode, modelParam, inbound)
		if err != nil {
			return adapterFailure(http.StatusBadRequest, err.Error())
		}
		originModel := request.Model
		request.Model = mappedModel
		if inbound == "" || inbound == native.OpenAIChat {
			setSystemPrompt(transport.Request.Context(), request, channel.SystemPrompt)
		}
		promptTokens := getPromptTokens(request, relayMode)
		requestMeta := providerMeta(channel, originModel, mappedModel, relayMode, request.Stream,
			promptTokens, transport.Request.URL.String(), principal)
		requestMeta.Inbound = inbound
		provider := relay.GetAdaptor(requestMeta.APIType)
		if provider == nil {
			return adapterFailure(http.StatusUnprocessableEntity, "selected channel has no adapter for this operation")
		}
		provider.Init(requestMeta)
		transport.Set(ctxkey.RequestModel, originModel)
		transport.Set(ctxkey.OriginalModel, originModel)
		requestBody, err := providerRequestBody(transport, requestMeta, request, body, provider)
		if err != nil {
			return adapterFailure(http.StatusUnprocessableEntity, err.Error())
		}
		response, err := provider.DoRequest(transport, requestMeta, requestBody)
		if err != nil {
			monitorProviderAttempt(channel, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "upstream request failed"}, nil)
			return adapterFailure(http.StatusBadGateway, "upstream request failed")
		}
		if isErrorHappened(requestMeta, response) {
			if response == nil {
				monitorProviderAttempt(channel, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "upstream response is missing"}, nil)
				return adapterFailure(http.StatusBadGateway, "upstream response is missing")
			}
			upstream := lifecycle.ReadUpstreamError(response)
			httpErr := &lifecycle.HTTPError{Status: upstream.Status, Message: upstream.Message, Upstream: &upstream}
			monitorProviderAttempt(channel, httpErr, &upstream)
			return lifecycle.AdapterResult{Err: httpErr}
		}
		usage, responseError := provider.DoResponse(transport, response, requestMeta)
		result := lifecycle.AdapterResult{OutputStarted: transport.Writer.Written()}
		if usage != nil {
			result.Usage = native.Usage{Input: int64(usage.PromptTokens), Output: int64(usage.CompletionTokens), Seen: usage.TotalTokens > 0, Complete: responseError == nil}
		}
		if responseError != nil {
			result.Err = &lifecycle.HTTPError{Status: responseError.StatusCode, Message: responseError.Message}
			if result.Err.Status == 0 {
				result.Err.Status = http.StatusBadGateway
			}
		} else if !result.OutputStarted {
			result.Err = &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "provider adapter produced no response"}
		}
		if result.Err == nil {
			monitorProviderAttempt(channel, nil, nil)
		}
		return result
	}
}

// parseProviderRequest parses the client wire for the provider converters:
// the Chat wire parses into the full TextRequest; the Anthropic and Responses
// wires only need the model and stream flag, with the raw body converted
// natively by the adapter.
func parseProviderRequest(body []byte, relayMode int, modelParam string, inbound native.Protocol) (*relaymodel.TextRequest, error) {
	if inbound == native.Anthropic || inbound == native.OpenAIResponses {
		textRequest := &relaymodel.TextRequest{}
		var fields struct {
			Model     string `json:"model"`
			Stream    bool   `json:"stream"`
			MaxTokens *int64 `json:"max_tokens"`
		}
		if err := json.Unmarshal(body, &fields); err != nil || fields.Model == "" {
			return nil, errors.New("model is required")
		}
		textRequest.Model = fields.Model
		textRequest.Stream = fields.Stream
		if fields.MaxTokens != nil {
			limit := int(*fields.MaxTokens)
			textRequest.MaxTokens = limit
		}
		return textRequest, nil
	}
	return parseTextRequest(body, relayMode, modelParam)
}

// RelayProviderMessages serves Anthropic Messages clients on the provider
// channels that keep adapters (Coze, Replicate); the raw body converts
// natively inside the adapter.
func RelayProviderMessages(c *gin.Context) *lifecycle.HTTPError {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	var fields struct {
		Model     string `json:"model"`
		Stream    bool   `json:"stream"`
		MaxTokens *int64 `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &fields); err != nil || fields.Model == "" || fields.MaxTokens == nil || *fields.MaxTokens <= 0 {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: "model and positive max_tokens are required"}
	}
	return relayProviderNativeWire(c, body, native.Anthropic, fields.Model, fields.Stream, *fields.MaxTokens)
}

// RelayProviderResponses serves Responses clients on the provider channels
// that keep adapters (Coze, Replicate); the raw body converts natively inside
// the adapter.
func RelayProviderResponses(c *gin.Context) *lifecycle.HTTPError {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	var fields struct {
		Model           string `json:"model"`
		Stream          bool   `json:"stream"`
		MaxOutputTokens *int64 `json:"max_output_tokens"`
	}
	if err := json.Unmarshal(body, &fields); err != nil || fields.Model == "" || fields.MaxOutputTokens == nil || *fields.MaxOutputTokens < 0 {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: "model and max_output_tokens are required"}
	}
	limit := max(*fields.MaxOutputTokens, int64(len(body)))
	return relayProviderNativeWire(c, body, native.OpenAIResponses, fields.Model, fields.Stream, limit)
}

// relayProviderNativeWire forwards one native-wire client request onto the
// provider adapter path with the shared lifecycle owning selection and retry.
func relayProviderNativeWire(c *gin.Context, body []byte, inbound native.Protocol, model string, stream bool, limit int64) *lifecycle.HTTPError {
	metadata := ginmeta.Get(c)
	specificID, httpErr := pinnedChannelID(c)
	if httpErr != nil {
		return httpErr
	}
	input := lifecycle.Request{
		Protocol: inbound, Model: model, Stream: stream,
		MaxOutputTokens: limit, Body: body, RetryLimit: config.RetryTimes,
		Channel:   providerChannel(metadata),
		Principal: lifecycle.Principal{UserID: metadata.UserId, TokenID: metadata.TokenId, TokenName: metadata.TokenName, Group: metadata.Group, SpecificChannel: specificID > 0, SpecificChannelID: specificID},
	}
	return lifecycle.ForwardAdapter(c.Request.Context(), input, providerTextAttempt(body, relaymode.ChatCompletions, "", inbound, input.Principal, providerTransport(c)))
}

// RelayProviderText serves completions and embeddings on provider channels
// through the shared lifecycle. Chat uses RelayProviderChat for its
// output-limit estimate; embeddings reserve the configured allowance.
func RelayProviderText(c *gin.Context, relayMode int) *lifecycle.HTTPError {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	parsed, err := parseTextRequest(body, relayMode, c.Param("model"))
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	metadata := ginmeta.Get(c)
	specificID, httpErr := pinnedChannelID(c)
	if httpErr != nil {
		return httpErr
	}
	input := lifecycle.Request{
		Protocol: native.OpenAIChat, Model: parsed.Model, Stream: parsed.Stream,
		MaxOutputTokens: 0, Body: body, RetryLimit: config.RetryTimes,
		Channel:   providerChannel(metadata),
		Principal: lifecycle.Principal{UserID: metadata.UserId, TokenID: metadata.TokenId, TokenName: metadata.TokenName, Group: metadata.Group, SpecificChannel: specificID > 0, SpecificChannelID: specificID},
	}
	return lifecycle.ForwardAdapter(c.Request.Context(), input, providerTextAttempt(body, relayMode, c.Param("model"), native.OpenAIChat, input.Principal, providerTransport(c)))
}

// RelayProviderChat retains provider-specific chat adapters while routing
// selection, retry, and billing through the shared lifecycle.
func RelayProviderChat(c *gin.Context) *lifecycle.HTTPError {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	parsed, err := parseTextRequest(body, relaymode.ChatCompletions, "")
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	limit := int64(parsed.MaxTokens)
	if parsed.MaxCompletionTokens != nil {
		if *parsed.MaxCompletionTokens < 0 {
			return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: "max_completion_tokens must not be negative"}
		}
		limit = max(limit, int64(*parsed.MaxCompletionTokens))
	}
	metadata := ginmeta.Get(c)
	specificID, httpErr := pinnedChannelID(c)
	if httpErr != nil {
		return httpErr
	}
	input := lifecycle.Request{
		Protocol: native.OpenAIChat, Model: parsed.Model, Stream: parsed.Stream,
		MaxOutputTokens: limit, Body: body, RetryLimit: config.RetryTimes,
		Channel:   providerChannel(metadata),
		Principal: lifecycle.Principal{UserID: metadata.UserId, TokenID: metadata.TokenId, TokenName: metadata.TokenName, Group: metadata.Group, SpecificChannel: specificID > 0, SpecificChannelID: specificID},
	}
	return lifecycle.ForwardAdapter(c.Request.Context(), input, providerTextAttempt(body, relaymode.ChatCompletions, "", native.OpenAIChat, input.Principal, providerTransport(c)))
}

// pinnedChannelID reports the dispatcher-pinned channel, if any.
func pinnedChannelID(c *gin.Context) (int, *lifecycle.HTTPError) {
	specificValue, pinned := c.Get(ctxkey.SpecificChannelId)
	if !pinned {
		return 0, nil
	}
	value, ok := specificValue.(string)
	if !ok {
		return 0, &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: "invalid channel ID"}
	}
	specificID, err := strconv.Atoi(value)
	if err != nil || specificID <= 0 {
		return 0, &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: "invalid channel ID"}
	}
	return specificID, nil
}

// providerImageAttempt runs one provider adapter attempt for image generation.
// Provider image responses carry no token usage; the exact operation charge
// was reserved as the fixed quota.
func providerImageAttempt(request *relaymodel.ImageRequest, principal lifecycle.Principal, transport *adaptor.Context) lifecycle.AdapterAttempt {
	return func(channel lifecycle.Channel, mappedModel string) lifecycle.AdapterResult {
		imageRequest := *request
		originModel := imageRequest.Model
		imageRequest.Model = mappedModel
		// Convert the original image model name (e.g. cogview) after the
		// channel's own mapping.
		imageRequest.Model, _ = getMappedModelName(imageRequest.Model, billingratio.ImageOriginModelName)
		requestMeta := providerMeta(channel, originModel, imageRequest.Model, relaymode.ImagesGenerations,
			false, 0, transport.Request.URL.String(), principal)
		provider := relay.GetAdaptor(requestMeta.APIType)
		if provider == nil {
			return adapterFailure(http.StatusUnprocessableEntity, "selected channel has no image adapter")
		}
		provider.Init(requestMeta)
		transport.Set("response_format", imageRequest.ResponseFormat)
		transport.Set(ctxkey.RequestModel, originModel)
		transport.Set(ctxkey.OriginalModel, originModel)

		requestBody, err := providerImageRequestBody(transport, &imageRequest, requestMeta, provider)
		if err != nil {
			return adapterFailure(http.StatusUnprocessableEntity, err.Error())
		}
		response, err := provider.DoRequest(transport, requestMeta, requestBody)
		if err != nil {
			monitorProviderAttempt(channel, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "upstream request failed"}, nil)
			return adapterFailure(http.StatusBadGateway, "upstream request failed")
		}
		if response == nil || response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
			if response == nil {
				monitorProviderAttempt(channel, &lifecycle.HTTPError{Status: http.StatusBadGateway, Message: "upstream response is missing"}, nil)
				return adapterFailure(http.StatusBadGateway, "upstream response is missing")
			}
			upstream := lifecycle.ReadUpstreamError(response)
			httpErr := &lifecycle.HTTPError{Status: upstream.Status, Message: upstream.Message, Upstream: &upstream}
			monitorProviderAttempt(channel, httpErr, &upstream)
			return lifecycle.AdapterResult{Err: httpErr}
		}
		_, responseError := provider.DoResponse(transport, response, requestMeta)
		result := lifecycle.AdapterResult{OutputStarted: transport.Writer.Written()}
		if responseError != nil {
			result.Err = &lifecycle.HTTPError{Status: responseError.StatusCode, Message: responseError.Message}
			if result.Err.Status == 0 {
				result.Err.Status = http.StatusBadGateway
			}
		}
		if result.Err == nil {
			monitorProviderAttempt(channel, nil, nil)
		}
		return result
	}
}

// RelayProviderImages serves the retained provider image format (Replicate)
// through the shared lifecycle with image-size billing; the retired vendor
// image kinds either ride the OpenAI wire or were migrated away at boot.
func RelayProviderImages(c *gin.Context) *lifecycle.HTTPError {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	imageRequest, err := parseImageRequest(body)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusBadRequest, Message: err.Error()}
	}
	metadata := ginmeta.Get(c)
	specificID, httpErr := pinnedChannelID(c)
	if httpErr != nil {
		return httpErr
	}

	// Map the model with the dispatcher-selected channel for validation and
	// pricing, exactly like the legacy controller did before forwarding.
	mappedModel, _ := getMappedModelName(imageRequest.Model, metadata.ModelMapping)
	imageRequest.Model = mappedModel
	if bizErr := validateImageRequest(imageRequest); bizErr != nil {
		return &lifecycle.HTTPError{Status: bizErr.StatusCode, Message: bizErr.Error.Message}
	}
	imageCostRatio, err := getImageCostRatio(imageRequest)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
	}
	modelRatio, err := billingratio.GetModelRatio(imageRequest.Model)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusForbidden, Message: err.Error()}
	}
	groupRatio := billingratio.GetGroupRatio(metadata.Group)
	ratio := modelRatio * groupRatio
	quota := int64(ratio*imageCostRatio*1000) * int64(imageRequest.N)
	if metadata.ChannelType == channeltype.Replicate {
		// replicate always returns 1 image
		quota = int64(ratio * imageCostRatio * 1000)
	}

	input := lifecycle.Request{
		Protocol: native.OpenAIImages, Model: imageRequest.Model,
		FixedQuota: quota, Body: body, RetryLimit: config.RetryTimes,
		Channel:   providerChannel(metadata),
		Principal: lifecycle.Principal{UserID: metadata.UserId, TokenID: metadata.TokenId, TokenName: metadata.TokenName, Group: metadata.Group, SpecificChannel: specificID > 0, SpecificChannelID: specificID},
	}
	return lifecycle.ForwardAdapter(c.Request.Context(), input, providerImageAttempt(imageRequest, input.Principal, providerTransport(c)))
}

// parseImageRequest parses and defaults an OpenAI images body.
func parseImageRequest(body []byte) (*relaymodel.ImageRequest, error) {
	imageRequest := &relaymodel.ImageRequest{}
	if err := json.Unmarshal(body, imageRequest); err != nil {
		return nil, err
	}
	if imageRequest.N == 0 {
		imageRequest.N = 1
	}
	if imageRequest.Size == "" {
		imageRequest.Size = "1024x1024"
	}
	if imageRequest.Model == "" {
		imageRequest.Model = "dall-e-2"
	}
	return imageRequest, nil
}

func isValidImageSize(model string, size string) bool {
	if model == "cogview-3" || billingratio.ImageSizeRatios[model] == nil {
		return true
	}
	_, ok := billingratio.ImageSizeRatios[model][size]
	return ok
}

func isValidImagePromptLength(model string, promptLength int) bool {
	maxPromptLength, ok := billingratio.ImagePromptLengthLimitations[model]
	return !ok || promptLength <= maxPromptLength
}

func isWithinRange(element string, value int) bool {
	amounts, ok := billingratio.ImageGenerationAmounts[element]
	return !ok || (value >= amounts[0] && value <= amounts[1])
}

func getImageSizeRatio(model string, size string) float64 {
	if ratio, ok := billingratio.ImageSizeRatios[model][size]; ok {
		return ratio
	}
	return 1
}

func validateImageRequest(imageRequest *relaymodel.ImageRequest) *relaymodel.ErrorWithStatusCode {
	// check prompt length
	if imageRequest.Prompt == "" {
		return openai.ErrorWrapper(io.EOF, "prompt_missing", http.StatusBadRequest)
	}

	// model validation
	if !isValidImageSize(imageRequest.Model, imageRequest.Size) {
		return openai.ErrorWrapper(io.EOF, "size_not_supported", http.StatusBadRequest)
	}

	if !isValidImagePromptLength(imageRequest.Model, len(imageRequest.Prompt)) {
		return openai.ErrorWrapper(io.EOF, "prompt_too_long", http.StatusBadRequest)
	}

	// number of generated images validation
	if !isWithinRange(imageRequest.Model, imageRequest.N) {
		return openai.ErrorWrapper(io.EOF, "n_not_within_range", http.StatusBadRequest)
	}
	return nil
}

func getImageCostRatio(imageRequest *relaymodel.ImageRequest) (float64, error) {
	if imageRequest == nil {
		return 0, io.EOF
	}
	imageCostRatio := getImageSizeRatio(imageRequest.Model, imageRequest.Size)
	if imageRequest.Quality == "hd" && imageRequest.Model == "dall-e-3" {
		if imageRequest.Size == "1024x1024" {
			imageCostRatio *= 2
		} else {
			imageCostRatio *= 1.5
		}
	}
	return imageCostRatio, nil
}

// providerRequestBody converts a request into the upstream body through the
// provider's own converter. Native-wire inbounds (Anthropic, Responses) carry
// their raw body so the adapter converts the client's actual wire directly.
func providerRequestBody(transport *adaptor.Context, requestMeta *meta.Meta, request *relaymodel.TextRequest, rawBody []byte, provider adaptor.Adaptor) (io.Reader, error) {
	conversion := &relaymodel.ConversionInput{Mode: requestMeta.Mode, Request: request, APIKey: requestMeta.APIKey,
		Inbound: requestMeta.Inbound, RawBody: rawBody}
	convertedRequest, err := provider.ConvertRequest(conversion)
	if err != nil {
		return nil, err
	}
	for key, value := range conversion.Values {
		transport.Set(key, value)
	}
	jsonData, err := json.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(jsonData), nil
}

// GenerateProviderImages drives one provider-format image generation
// (currently the retained Replicate format) headlessly for the studio image
// workbench: the caller has already selected the channel and computed the
// fixed image quota; the attempt runs under the shared lifecycle and the
// OpenAI-shaped response lands in dst instead of a relay client connection.
func GenerateProviderImages(ctx context.Context, imageRequest *relaymodel.ImageRequest,
	principal lifecycle.Principal, channel lifecycle.Channel, fixedQuota int64, dst http.ResponseWriter,
) *lifecycle.HTTPError {
	synthetic, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/images/generations", http.NoBody)
	if err != nil {
		return &lifecycle.HTTPError{Status: http.StatusInternalServerError, Message: err.Error()}
	}
	transport := adaptor.NewContext(synthetic, dst)
	requestID := random.GetUUID()
	transport.Set(helper.RequestIdKey, requestID)
	transport.Set("response_format", imageRequest.ResponseFormat)
	transport.Set(ctxkey.RequestModel, imageRequest.Model)
	transport.Set(ctxkey.OriginalModel, imageRequest.Model)
	attempt := providerImageAttempt(imageRequest, principal, transport)
	input := lifecycle.Request{
		Protocol: native.OpenAIImages, Model: imageRequest.Model,
		FixedQuota: fixedQuota, RetryLimit: config.RetryTimes,
		Channel:   channel,
		Principal: principal,
	}
	return lifecycle.ForwardAdapter(ctx, input, attempt)
}

// providerImageRequestBody builds the upstream body for provider image
// formats: Replicate's converter produces the provider wire format, other
// provider types forward the (mapped) image body unchanged.
func providerImageRequestBody(transport *adaptor.Context, imageRequest *relaymodel.ImageRequest, requestMeta *meta.Meta, provider adaptor.Adaptor) (io.Reader, error) {
	jsonStr, err := json.Marshal(imageRequest)
	if err != nil {
		return nil, err
	}
	switch requestMeta.ChannelType {
	case channeltype.Replicate:
		finalRequest, err := provider.ConvertImageRequest(imageRequest)
		if err != nil {
			return nil, err
		}
		jsonStr, err = json.Marshal(finalRequest)
		if err != nil {
			return nil, err
		}
	}
	return bytes.NewReader(jsonStr), nil
}

// adapterFailure wraps an attempt failure.
func adapterFailure(status int, message string) lifecycle.AdapterResult {
	return lifecycle.AdapterResult{Err: &lifecycle.HTTPError{Status: status, Message: message}}
}
