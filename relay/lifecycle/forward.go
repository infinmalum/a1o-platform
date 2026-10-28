package lifecycle

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/billing"
	"github.com/infinmalum/a1o-platform/relay/bridge"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// Channel contains only upstream transport settings. Account and token data
// remain separate so a retry can replace the channel without changing the user.
type Channel struct {
	ID           int
	Type         int
	BaseURL      string
	APIKey       string
	ModelMapping map[string]string
	SystemPrompt string
	APIVersion   string
	// Config carries provider-specific channel settings such as VertexAI
	// credentials, AWS region settings, and per-provider user IDs.
	Config model.ChannelConfig
	// UpstreamAccountID binds the channel to an administrator-connected
	// subscription account; each attempt resolves its lease and credential.
	UpstreamAccountID int
}

type Principal struct {
	UserID            int
	TokenID           int
	TokenName         string
	Group             string
	SpecificChannel   bool
	SpecificChannelID int
}

type Request struct {
	Protocol         native.Protocol
	UpstreamProtocol native.Protocol
	// FallbackUpstreamProtocols is tried in order when no channel for the
	// primary upstream protocol can serve the model.
	FallbackUpstreamProtocols []native.Protocol
	Model                     string
	Action                    string
	Version                   string
	Stream                    bool
	MaxOutputTokens           int64
	// FallbackInputTokens is used by operations whose successful response may
	// omit usage, such as Moderations. A provider usage report takes precedence.
	FallbackInputTokens int64
	// FixedQuota charges an operation-specific amount, such as image size
	// pricing or audio input length, instead of settling from provider token
	// usage. ResponseCharge optionally derives the final charge from the
	// buffered response; FixedPromptTokens reports the charged amount as the
	// consume log's PromptTokens, matching the legacy audio behavior.
	FixedQuota        int64
	FixedPromptTokens bool
	ResponseCharge    func(header http.Header, body []byte) (charge int64, promptTokens int64, err error)
	Body              []byte
	Headers           http.Header
	Query             url.Values
	Channel           Channel
	Principal         Principal
	RetryLimit        int
	HTTPClient        *http.Client
}

type HTTPError struct {
	Status   int
	Message  string
	Upstream *UpstreamError
}

// Forward executes native passthrough and explicit JSON protocol conversions.
// It owns reservation, retries, response copying, and settlement; the caller
// owns request parsing and the client protocol's error envelope.
func Forward(ctx context.Context, dst http.ResponseWriter, input Request) *HTTPError {
	upstreamProtocol := input.Protocol
	if input.UpstreamProtocol != "" {
		upstreamProtocol = input.UpstreamProtocol
	}
	if input.Principal.Group == "" {
		group, err := model.CacheGetUserGroup(input.Principal.UserID)
		if err != nil {
			return &HTTPError{Status: http.StatusInternalServerError, Message: "failed to load user group"}
		}
		input.Principal.Group = group
	}
	if input.Channel.ID == 0 {
		var primaryErr *HTTPError
		var selected Channel
		candidates := append([]native.Protocol{upstreamProtocol}, input.FallbackUpstreamProtocols...)
		source := InitialSource()
		selected, upstreamProtocol, primaryErr = selectInitialForProtocols(input, candidates, source)
		if selected.ID == 0 && primaryErr != nil && primaryErr.Status == http.StatusServiceUnavailable &&
			source == model.ChannelSourceSubscriptionOnly &&
			config.UpstreamRoutingPolicy == config.RoutingPolicySubscriptionThenAPI {
			// No eligible subscription exists; the explicit fallback policy
			// allows this request to run on API channels. API channels prefer
			// the client's native protocol: the primary candidate may be a
			// bridge target that only subscriptions serve, such as Responses
			// for Chat inbound under subscription-first routing.
			apiCandidates := []native.Protocol{input.Protocol}
			for _, candidate := range candidates {
				if candidate != input.Protocol {
					apiCandidates = append(apiCandidates, candidate)
				}
			}
			selected, upstreamProtocol, primaryErr = selectInitialForProtocols(input, apiCandidates, model.ChannelSourceAPIOnly)
		}
		if selected.ID == 0 {
			return primaryErr
		}
		input.Channel = selected
	} else if !channelSupportsProtocol(input.Channel.Type, upstreamProtocol) {
		matched := false
		for _, candidate := range input.FallbackUpstreamProtocols {
			if channelSupportsProtocol(input.Channel.Type, candidate) {
				upstreamProtocol = candidate
				matched = true
				break
			}
		}
		if !matched {
			return &HTTPError{Status: http.StatusBadRequest, Message: "selected channel does not support this protocol"}
		}
	}
	converter, err := bridge.For(input.Protocol, upstreamProtocol)
	if err != nil {
		return &HTTPError{Status: http.StatusUnprocessableEntity, Message: err.Error()}
	}
	var streamConverter bridge.StreamConverter
	if converter != nil && input.Stream {
		candidate, ok := converter.(bridge.StreamConverter)
		if !ok {
			return &HTTPError{Status: http.StatusUnprocessableEntity, Message: "streaming protocol conversion is not supported"}
		}
		streamConverter = candidate
	}
	httpClient := input.HTTPClient
	if httpClient == nil {
		httpClient = client.HTTPClient
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	providerClient := *httpClient
	providerClient.CheckRedirect = func(next *http.Request, previous []*http.Request) error {
		if len(previous) >= 3 || next.URL.Scheme != previous[0].URL.Scheme || !strings.EqualFold(next.URL.Host, previous[0].URL.Host) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	settlementContext := context.WithoutCancel(ctx)
	retries := max(0, input.RetryLimit)
	tried := make(map[int]bool)
	// Accounts already refreshed within this request: the pre-flight refresh
	// and the 401 replay each happen at most once per account.
	refreshed := make(map[int]bool)
	channel := input.Channel
	// The Gemini action is selected by the transport, so a fallback onto a
	// Gemini channel derives it from the final upstream protocol instead of
	// requiring the controller to know the routing outcome.
	action := input.Action
	if upstreamProtocol == native.Gemini {
		action = "generateContent"
		if input.Stream {
			action = "streamGenerateContent"
		}
	}
	for {
		tried[channel.ID] = true
		mappedModel := input.Model
		if replacement := channel.ModelMapping[input.Model]; replacement != "" {
			mappedModel = replacement
		}
		var subscription *subscriptionAttempt
		if channel.UpstreamAccountID != 0 {
			var httpErr *HTTPError
			subscription, httpErr = prepareSubscription(ctx, channel, upstreamProtocol, refreshed)
			if httpErr != nil {
				// The account cannot serve this attempt; fail over under the
				// routing policy's retry preferences.
				if next, ok := selectRetry(ctx, input, channel, tried, &retries); ok {
					channel = next
					continue
				}
				return httpErr
			}
		}
		attempt := channel
		outcome := func() attemptOutcome {
			if subscription != nil {
				// The lease covers the entire upstream operation and is
				// released on every exit path.
				defer subscription.Lease.Release()
			}
			version := input.Version
			if attempt.APIVersion != "" {
				version = attempt.APIVersion
			}
			body := input.Body
			if converter != nil {
				var err error
				body, err = converter.Request(body, mappedModel)
				if err != nil {
					return attemptOutcome{err: &HTTPError{Status: http.StatusUnprocessableEntity, Message: err.Error()}}
				}
				if input.Stream && upstreamProtocol == native.OpenAIChat {
					// Inbound protocols such as Gemini carry the stream intent
					// in the URL action; the Chat wire needs it in the body
					// with the usage report option. Idempotent for converters
					// that already set the fields.
					body, err = bridge.EnsureChatStreamOptions(body)
					if err != nil {
						return attemptOutcome{err: &HTTPError{Status: http.StatusUnprocessableEntity, Message: err.Error()}}
					}
				}
			}
			apiKey := attempt.APIKey
			if subscription != nil {
				if subscription.validator != nil {
					normalized, validateErr := subscription.validator(body)
					if validateErr != nil {
						// The request is outside this profile's capability
						// matrix; another channel may still serve it.
						capabilityErr := &HTTPError{Status: http.StatusUnprocessableEntity, Message: validateErr.Error()}
						if next, ok := selectRetry(ctx, input, attempt, tried, &retries); ok {
							return attemptOutcome{retry: true, retryWith: next}
						}
						return attemptOutcome{err: capabilityErr}
					}
					body = normalized
				}
				apiKey = subscription.APIKey
			}
			req, err := native.BuildRequest(ctx, native.Request{
				Protocol: upstreamProtocol, ChannelType: attempt.Type, BaseURL: attempt.BaseURL, Version: version,
				Model: mappedModel, Action: action, APIKey: apiKey,
				SystemPrompt: attempt.SystemPrompt, Body: body,
				Headers: input.Headers, Query: input.Query,
			})
			if err != nil {
				return attemptOutcome{err: &HTTPError{Status: http.StatusBadRequest, Message: err.Error()}}
			}
			reservation, err := reserve(ctx, input, attempt, mappedModel, upstreamProtocol)
			if err != nil {
				return attemptOutcome{err: &HTTPError{Status: http.StatusForbidden, Message: err.Error()}}
			}
			response, err := providerClient.Do(req)
			if err != nil {
				reservation.Refund(settlementContext)
				logger.Errorf(settlementContext, "upstream request failed on channel %d: %v", attempt.ID, err)
				if next, ok := selectRetry(ctx, input, attempt, tried, &retries); ok {
					return attemptOutcome{retry: true, retryWith: next}
				}
				return attemptOutcome{err: &HTTPError{Status: http.StatusBadGateway, Message: "upstream request failed"}}
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				reservation.Refund(settlementContext)
				if response.StatusCode >= 300 && response.StatusCode < 400 {
					_ = response.Body.Close()
					return attemptOutcome{err: &HTTPError{Status: http.StatusBadGateway, Message: "upstream redirect is not allowed"}}
				}
				if response.StatusCode == http.StatusTooManyRequests && attempt.UpstreamAccountID != 0 {
					// Respect the provider reset window so selection skips
					// this account until the cooldown expires. The cooldown
					// is keyed by the client-facing model: channel selection
					// checks readiness before ModelMapping is applied, so a
					// mapped name would never match the lookup and the 429'd
					// account would keep being selected.
					coolDownAccount(attempt.UpstreamAccountID, input.Model, response.Header)
				}
				if response.StatusCode == http.StatusUnauthorized && attempt.UpstreamAccountID != 0 {
					// One bounded refresh/replay per account, within the
					// attempt budget, before any output reached the client.
					if !refreshed[attempt.UpstreamAccountID] && retries > 0 && ctx.Err() == nil {
						if _, refreshErr := refreshAccountCredential(ctx, subscription.ProfileID, attempt.UpstreamAccountID); refreshErr == nil {
							refreshed[attempt.UpstreamAccountID] = true
							retries--
							_ = response.Body.Close()
							return attemptOutcome{retry: true, retryWith: attempt}
						}
					}
					// The credential is unusable; other channels may serve.
					if next, ok := selectRetry(ctx, input, attempt, tried, &retries); ok {
						_ = response.Body.Close()
						return attemptOutcome{retry: true, retryWith: next}
					}
				}
				if Retryable(response.StatusCode, false, input.Principal.SpecificChannel, ctx.Err() != nil) {
					if next, ok := selectRetry(ctx, input, attempt, tried, &retries); ok {
						_ = response.Body.Close()
						return attemptOutcome{retry: true, retryWith: next}
					}
				}
				if converter != nil {
					upstreamError := ReadUpstreamError(response)
					copyHeaders(dst.Header(), response.Header)
					return attemptOutcome{err: &HTTPError{Status: response.StatusCode, Message: upstreamError.Message, Upstream: &upstreamError}}
				}
				copyHeaders(dst.Header(), response.Header)
				dst.WriteHeader(response.StatusCode)
				_, _ = io.Copy(dst, response.Body)
				_ = response.Body.Close()
				return attemptOutcome{}
			}
			if input.Stream && !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
				reservation.Refund(settlementContext)
				_ = response.Body.Close()
				return attemptOutcome{err: &HTTPError{Status: http.StatusBadGateway, Message: "upstream did not return an event stream"}}
			}
			if input.FixedQuota > 0 {
				return attemptOutcome{err: settleFixedQuota(ctx, settlementContext, dst, response, input, reservation, attempt)}
			}
			if streamConverter != nil {
				copyHeaders(dst.Header(), response.Header)
				dst.Header().Set("Content-Type", "text/event-stream")
				dst.Header().Del("Content-Length")
				dst.WriteHeader(http.StatusOK)
				usage, streamErr := streamConverter.Stream(flushingWriter{dst}, response.Body, input.Model)
				_ = response.Body.Close()
				interrupted := streamErr != nil || ctx.Err() != nil || !usage.Complete
				reservation.Settle(settlementContext, usage, true, interrupted)
				if streamErr != nil {
					logger.Errorf(settlementContext, "converted stream interrupted on channel %d: %v", attempt.ID, streamErr)
				}
				return attemptOutcome{}
			}
			if converter != nil {
				const maxConvertedResponse = 16 << 20
				body, readErr := io.ReadAll(io.LimitReader(response.Body, maxConvertedResponse+1))
				_ = response.Body.Close()
				if readErr != nil || len(body) > maxConvertedResponse {
					reservation.Settle(settlementContext, native.Usage{}, false, true)
					return attemptOutcome{err: &HTTPError{Status: http.StatusBadGateway, Message: "upstream response could not be read"}}
				}
				converted, usage, convertErr := converter.Response(body, input.Model)
				if convertErr != nil {
					reservation.Settle(settlementContext, native.Usage{}, false, true)
					return attemptOutcome{err: &HTTPError{Status: http.StatusBadGateway, Message: convertErr.Error()}}
				}
				copyHeaders(dst.Header(), response.Header)
				dst.Header().Set("Content-Type", "application/json")
				dst.Header().Del("Content-Length")
				dst.WriteHeader(http.StatusOK)
				_, writeErr := dst.Write(converted)
				reservation.Settle(settlementContext, usage, false, writeErr != nil || ctx.Err() != nil)
				if writeErr != nil {
					logger.Errorf(settlementContext, "converted response interrupted on channel %d: %v", attempt.ID, writeErr)
				}
				return attemptOutcome{}
			}
			copyHeaders(dst.Header(), response.Header)
			dst.WriteHeader(response.StatusCode)
			usage, copyErr := native.CopyResponse(dst, response.Body, input.Protocol, input.Stream)
			_ = response.Body.Close()
			if copyErr == nil && usage.Complete && !usage.Seen && input.FallbackInputTokens > 0 {
				usage.Input = input.FallbackInputTokens
				usage.Seen = true
			}
			interrupted := copyErr != nil || ctx.Err() != nil || (input.Stream && !usage.Complete)
			reservation.Settle(settlementContext, usage, input.Stream, interrupted)
			if copyErr != nil {
				logger.Errorf(settlementContext, "upstream response interrupted on channel %d: %v", attempt.ID, copyErr)
			}
			return attemptOutcome{}
		}()
		if outcome.retry {
			channel = outcome.retryWith
			continue
		}
		return outcome.err
	}
}

func supportsSystemPrompt(protocol native.Protocol) bool {
	switch protocol {
	case native.Anthropic, native.Gemini, native.OpenAIChat, native.OpenAIResponses, native.OpenAICompletions:
		return true
	default:
		return false
	}
}

// reserve creates the per-attempt reservation for native forwarding: an
// exact operation charge for fixed-quota operations, or the output-limit
// estimate for token billing. The system prompt only joins the reserved
// input estimate when the negotiated protocol can actually carry one.
func reserve(ctx context.Context, input Request, channel Channel, mappedModel string, upstreamProtocol native.Protocol) (*billing.NativeReservation, error) {
	return reserveFor(ctx, input, channel, mappedModel,
		channel.SystemPrompt != "" && supportsSystemPrompt(upstreamProtocol))
}

// reserveFor is the shared reservation entry for both attempt styles; callers
// state explicitly whether the channel's system prompt participates in the
// reserved input estimate.
func reserveFor(ctx context.Context, input Request, channel Channel, mappedModel string, systemPromptReset bool) (*billing.NativeReservation, error) {
	payload := billing.NativeReservation{
		UserID: input.Principal.UserID, TokenID: input.Principal.TokenID,
		ChannelID: channel.ID, ChannelType: channel.Type,
		TokenName: input.Principal.TokenName, ModelName: mappedModel,
		Group: input.Principal.Group, SystemPromptReset: systemPromptReset,
	}
	if input.FixedQuota > 0 {
		return billing.ReserveFixedQuota(ctx, &payload, input.FixedQuota)
	}
	return billing.ReserveNativeQuota(ctx, &payload, input.MaxOutputTokens)
}

// settleFixedQuota streams a fixed-charge response to the client and charges
// the operation amount: the buffered charge when ResponseCharge derives one
// from the response body, otherwise the reserved amount exactly.
func settleFixedQuota(requestCtx, settlementCtx context.Context, dst http.ResponseWriter,
	response *http.Response, input Request, reservation *billing.NativeReservation, channel Channel,
) *HTTPError {
	charge := input.FixedQuota
	promptTokens := int64(0)
	if input.ResponseCharge != nil {
		const maxChargedResponse = 64 << 20
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxChargedResponse))
		_ = response.Body.Close()
		if readErr != nil || len(payload) >= maxChargedResponse {
			reservation.Refund(settlementCtx)
			logger.Errorf(settlementCtx, "fixed-quota response could not be read on channel %d: %v", channel.ID, readErr)
			return &HTTPError{Status: http.StatusBadGateway, Message: "upstream response could not be read"}
		}
		derived, tokens, chargeErr := input.ResponseCharge(response.Header, payload)
		if chargeErr != nil {
			reservation.Refund(settlementCtx)
			logger.Errorf(settlementCtx, "fixed-quota response charge failed on channel %d: %v", channel.ID, chargeErr)
			return &HTTPError{Status: http.StatusBadGateway, Message: chargeErr.Error()}
		}
		if derived >= 0 {
			charge, promptTokens = derived, tokens
		}
		copyHeaders(dst.Header(), response.Header)
		dst.WriteHeader(response.StatusCode)
		_, writeErr := dst.Write(payload)
		interrupted := writeErr != nil || requestCtx.Err() != nil
		if interrupted {
			charge = max(charge, input.FixedQuota)
			if input.FixedPromptTokens {
				promptTokens = charge
			}
		}
		reservation.SettleDirect(settlementCtx, charge, promptTokens, interrupted)
		if writeErr != nil {
			logger.Errorf(settlementCtx, "fixed-quota response interrupted on channel %d: %v", channel.ID, writeErr)
		}
		return nil
	}
	copyHeaders(dst.Header(), response.Header)
	dst.WriteHeader(response.StatusCode)
	_, copyErr := io.Copy(dst, response.Body)
	_ = response.Body.Close()
	interrupted := copyErr != nil || requestCtx.Err() != nil
	if input.FixedPromptTokens {
		promptTokens = charge
	}
	reservation.SettleDirect(settlementCtx, charge, promptTokens, interrupted)
	if copyErr != nil {
		logger.Errorf(settlementCtx, "fixed-quota response interrupted on channel %d: %v", channel.ID, copyErr)
	}
	return nil
}

// flushingWriter lets bridge converters stream increments through the Response
// Writer without depending on Gin.
type flushingWriter struct {
	writer http.ResponseWriter
}

func (f flushingWriter) Write(p []byte) (int, error) {
	return f.writer.Write(p)
}

func (f flushingWriter) Flush() {
	if flusher, ok := f.writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

// SelectForRequest resolves and validates the initial channel for a request
// without executing it. Deferred executions such as background Responses use
// it to fail on channel problems before returning to the client.
func SelectForRequest(input Request, upstreamProtocol native.Protocol) (Channel, *HTTPError) {
	if input.UpstreamProtocol != "" {
		upstreamProtocol = input.UpstreamProtocol
	}
	if input.Channel.ID != 0 {
		return input.Channel, nil
	}
	if input.Principal.Group == "" {
		group, err := model.CacheGetUserGroup(input.Principal.UserID)
		if err != nil {
			return Channel{}, &HTTPError{Status: http.StatusInternalServerError, Message: "failed to load user group"}
		}
		input.Principal.Group = group
	}
	channel, selectErr := selectInitial(input, upstreamProtocol, InitialSource())
	if selectErr != nil {
		return Channel{}, selectErr
	}
	return channel, nil
}

// selectInitialForProtocols tries each candidate protocol once and returns
// the first selection that succeeds. Non-availability errors surface
// immediately except for a pinned channel's protocol mismatch.
func selectInitialForProtocols(input Request, candidates []native.Protocol, source model.ChannelSource) (Channel, native.Protocol, *HTTPError) {
	var primaryErr *HTTPError
	for _, candidate := range candidates {
		channel, err := selectInitial(input, candidate, source)
		if err == nil {
			return channel, candidate, nil
		}
		if primaryErr == nil {
			primaryErr = err
			if err.Status != http.StatusServiceUnavailable && !(input.Principal.SpecificChannel && err.Status == http.StatusBadRequest) {
				return Channel{}, candidate, err
			}
		}
	}
	return Channel{}, "", primaryErr
}

func selectInitial(input Request, upstreamProtocol native.Protocol, source model.ChannelSource) (Channel, *HTTPError) {
	requiredTypes := candidateChannelTypes(upstreamProtocol)
	if len(requiredTypes) == 0 {
		return Channel{}, &HTTPError{Status: http.StatusBadRequest, Message: "unsupported native protocol"}
	}
	specificID := 0
	if input.Principal.SpecificChannel {
		specificID = input.Principal.SpecificChannelID
		if specificID <= 0 {
			return Channel{}, &HTTPError{Status: http.StatusBadRequest, Message: "invalid channel ID"}
		}
	}
	selected, selectErr := SelectChannel(Selection{Group: input.Principal.Group, Model: input.Model, Types: requiredTypes, SpecificID: specificID, Source: source})
	if selectErr != nil {
		if selectErr.Status == http.StatusServiceUnavailable {
			label := "OpenAI"
			switch upstreamProtocol {
			case native.Anthropic:
				label = "Anthropic"
			case native.Gemini:
				label = "Gemini"
			}
			selectErr.Message = "no compatible " + label + " channel for this model"
		}
		return Channel{}, selectErr
	}
	var err error
	channel, err := channelFromModel(selected)
	if err != nil {
		return Channel{}, &HTTPError{Status: http.StatusInternalServerError, Message: "failed to load channel configuration"}
	}
	return channel, nil
}

func requiredChannelType(protocol native.Protocol) int {
	types := candidateChannelTypes(protocol)
	if len(types) == 0 {
		return -1
	}
	return types[0]
}

// candidateChannelTypes lists every channel kind that can serve a native
// protocol, most specific first. Chat-family protocols accept the whole
// OpenAI-compatible whitelist plus the Responses channel kind (the chat
// request is bridged onto /v1/responses); Responses requests land on a
// dedicated Responses channel or a plain OpenAI channel, whose /v1/responses
// path passes through on the same wire. Vendors expose Completions and
// Responses under different base URLs, so a channel holding a Completions
// URL stays out of the Responses candidates.
func candidateChannelTypes(protocol native.Protocol) []int {
	switch protocol {
	case native.Anthropic:
		return []int{channeltype.Anthropic}
	case native.Gemini:
		return []int{channeltype.Gemini}
	case native.OpenAIChat:
		return append(chatCompatibleChannelTypes, channeltype.OpenAIResponses)
	case native.OpenAICompletions, native.OpenAIEmbeddings, native.OpenAIModerations,
		native.OpenAIEdits, native.OpenAIImages, native.OpenAIAudioSpeech, native.OpenAIAudioTranscriptions, native.OpenAIAudioTranslations:
		return chatCompatibleChannelTypes
	case native.OpenAIResponses:
		return []int{channeltype.OpenAI, channeltype.OpenAIResponses}
	case native.SystemOne:
		return []int{channeltype.TypeSafe}
	default:
		return nil
	}
}

// chatCompatibleChannelTypes enumerates the NativeChatCompatible whitelist as
// channel kinds so protocol selection can query `type IN (...)` in one pass.
var chatCompatibleChannelTypes = func() []int {
	// Built from the whitelist so the two can never drift; the OpenAI kind
	// leads the list and doubles as the historical single-type match.
	types := make([]int, 0, 40)
	for kind := channeltype.OpenAI; kind < channeltype.Dummy; kind++ {
		if kind == channeltype.OpenAIResponses {
			continue
		}
		if channeltype.NativeChatCompatible(kind) {
			types = append(types, kind)
		}
	}
	return types
}()

func channelSupportsProtocol(kind int, protocol native.Protocol) bool {
	switch protocol {
	// The OpenAI wire protocols run on every channel type that speaks the
	// OpenAI JSON format, including Azure deployment URLs; a Chat request may
	// additionally land on a Responses channel through the chat bridge.
	case native.OpenAIChat:
		return channeltype.NativeChatCompatible(kind) || kind == channeltype.OpenAIResponses
	case native.OpenAICompletions, native.OpenAIEmbeddings, native.OpenAIModerations,
		native.OpenAIEdits, native.OpenAIImages, native.OpenAIAudioSpeech, native.OpenAIAudioTranscriptions, native.OpenAIAudioTranslations:
		return channeltype.NativeChatCompatible(kind)
	case native.OpenAIResponses:
		return kind == channeltype.OpenAI || kind == channeltype.OpenAIResponses
	}
	return kind == requiredChannelType(protocol)
}

func selectRetry(ctx context.Context, input Request, failed Channel, tried map[int]bool, remaining *int) (Channel, bool) {
	if *remaining <= 0 || ctx.Err() != nil || input.Principal.SpecificChannel {
		return Channel{}, false
	}
	// Candidates come from the routing policy's retry preference order, for
	// example remaining subscriptions before API channels.
	for _, source := range RetrySources() {
		selected, selectErr := SelectChannel(Selection{Group: input.Principal.Group, Model: input.Model, Type: failed.Type, Excluded: tried, Source: source})
		if selectErr != nil {
			continue
		}
		next, err := channelFromModel(selected)
		if err != nil {
			logger.Errorf(ctx, "failed to load retry channel %d: %v", selected.Id, err)
			return Channel{}, false
		}
		*remaining = *remaining - 1
		logger.Infof(ctx, "retrying request on channel %d after channel %d", next.ID, failed.ID)
		return next, true
	}
	return Channel{}, false
}

func channelFromModel(channel *model.Channel) (Channel, error) {
	if channel == nil || !channeltype.IsValid(channel.Type) {
		return Channel{}, errors.New("invalid channel type")
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = channeltype.BaseURL(channel.Type)
	}
	config, err := channel.LoadConfig()
	if err != nil {
		return Channel{}, err
	}
	if config.APIVersion == "" && channel.Type == channeltype.Gemini && channel.Other != nil {
		config.APIVersion = *channel.Other
	}
	selected := Channel{
		ID: channel.Id, Type: channel.Type, BaseURL: baseURL, APIKey: channel.Key,
		ModelMapping: channel.GetModelMapping(), APIVersion: config.APIVersion,
		Config: config,
	}
	if channel.SystemPrompt != nil {
		selected.SystemPrompt = *channel.SystemPrompt
	}
	if channel.UpstreamAccountID != nil {
		selected.UpstreamAccountID = *channel.UpstreamAccountID
	}
	return selected, nil
}

func copyHeaders(dst, src http.Header) {
	for _, name := range []string{"Content-Type", "Request-Id", "X-Request-Id", "Retry-After", "Anthropic-Version", "X-Goog-Request-Id", "OpenAI-Processing-Ms", "OpenAI-Version"} {
		for _, value := range src.Values(name) {
			dst.Add(name, value)
		}
	}
}
