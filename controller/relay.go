package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	relaycontroller "github.com/infinmalum/a1o-platform/relay/controller"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/native"
	"github.com/infinmalum/a1o-platform/relay/relaymode"
)

// openAIWireChannel reports whether the channel selected by the dispatcher
// serves the relay mode on the OpenAI JSON wire, so the route can use the
// native lifecycle instead of a provider adapter.
func openAIWireChannel(c *gin.Context, mode int) bool {
	return channeltype.OpenAIWireSupportsOperation(c.GetInt(ctxkey.Channel), mode)
}

// wireChannelWithoutOperation reports an OpenAI wire channel whose compatible
// endpoints do not serve the requested relay mode, so the route rejects it
// instead of forwarding to an upstream path that does not exist.
func wireChannelWithoutOperation(c *gin.Context, mode int) bool {
	kind := c.GetInt(ctxkey.Channel)
	return channeltype.NativeChatCompatible(kind) && !channeltype.OpenAIWireSupportsOperation(kind, mode)
}

func unsupportedOperation(c *gin.Context, message string) {
	c.JSON(http.StatusUnprocessableEntity, native.ErrorBody(native.OpenAIChat, http.StatusUnprocessableEntity, message))
}

// RelayChat dispatches Chat Completions onto the native lifecycle for every
// kind whose upstream wire the lifecycle serves: OpenAI-compatible channels
// pass through, Anthropic, Gemini, AWS Bedrock, and Cloudflare channels are
// bridged onto their native wire, VertexAI channels resolve their wire from
// the model family, Responses-wire channels (which have no provider adapter)
// are bridged onto /v1/responses, and the retained provider adapters (Coze,
// Replicate) serve their channel types.
func RelayChat(c *gin.Context) {
	channelType := c.GetInt(ctxkey.Channel)
	switch {
	case channeltype.NativeChatCompatible(channelType):
		NativeOpenAIChat(c)
	case channelType == channeltype.Anthropic, channelType == channeltype.AwsClaude:
		NativeOpenAIChatViaAnthropic(c)
	case channelType == channeltype.Gemini:
		NativeOpenAIChatViaGemini(c)
	case channelType == channeltype.Cloudflare:
		NativeOpenAIChat(c)
	case channelType == channeltype.VertextAI:
		// The lifecycle aligns the wire with the model family after channel
		// selection; Anthropic is the declared preference so claude-* models
		// take the passthrough-style bridge.
		NativeOpenAIChatViaAnthropic(c)
	case channelType == channeltype.OpenAIResponses || channelType == channeltype.Vercel:
		nativeOpenAIChat(c, native.OpenAIResponses)
	default:
		writeProviderError(c, relaycontroller.RelayProviderChat(c))
	}
}

// RelayEmbeddings serves OpenAI wire channels and Cloudflare's compat wire
// natively; Gemini keeps its provider conversion, and every other provider
// kind is rejected instead of forwarding to an endpoint that does not exist.
func RelayEmbeddings(c *gin.Context) {
	if openAIWireChannel(c, relaymode.Embeddings) {
		NativeOpenAIEmbeddings(c)
		return
	}
	kind := c.GetInt(ctxkey.Channel)
	if kind == channeltype.Gemini {
		writeProviderError(c, relaycontroller.RelayProviderText(c, relaymode.Embeddings))
		return
	}
	unsupportedOperation(c, "this channel type does not serve the embeddings operation")
}

// RelayModerations serves OpenAI wire channels natively and rejects provider
// channels, whose adapters have no moderation format to convert into.
func RelayModerations(c *gin.Context) {
	switch {
	case openAIWireChannel(c, relaymode.Moderations):
		NativeOpenAIModerations(c)
	case wireChannelWithoutOperation(c, relaymode.Moderations):
		unsupportedOperation(c, "this channel type does not serve the moderations operation")
	default:
		unsupportedOperation(c, "moderations require an OpenAI-compatible channel")
	}
}

// RelayImages serves OpenAI wire channels through the native lifecycle with
// image-specific billing; the retained provider image format (Replicate)
// keeps its adapter conversion.
func RelayImages(c *gin.Context) {
	switch {
	case openAIWireChannel(c, relaymode.ImagesGenerations):
		NativeOpenAIImages(c)
	case wireChannelWithoutOperation(c, relaymode.ImagesGenerations):
		unsupportedOperation(c, "this channel type does not serve the images operation")
	default:
		writeProviderError(c, relaycontroller.RelayProviderImages(c))
	}
}

// writeProviderError presents a provider-route failure in the OpenAI error
// envelope, preserving upstream details when the adapter reported them.
func writeProviderError(c *gin.Context, result *lifecycle.HTTPError) {
	if result == nil {
		return
	}
	if c.Writer.Written() {
		return
	}
	if result.Upstream != nil {
		c.JSON(result.Status, native.ErrorBodyWithUpstream(native.OpenAIChat, result.Status, result.Message,
			result.Upstream.Type, result.Upstream.Code, result.Upstream.Param))
		return
	}
	c.JSON(result.Status, native.ErrorBody(native.OpenAIChat, result.Status, result.Message))
}

// RelayAudio serves the three audio operations on OpenAI wire channels,
// including their Azure deployment variants, and rejects provider channels
// and the retired vendor endpoints, none of which speak the audio wire.
func RelayAudio(c *gin.Context) {
	if !openAIWireChannel(c, relaymode.AudioSpeech) {
		if wireChannelWithoutOperation(c, relaymode.AudioSpeech) {
			unsupportedOperation(c, "this channel type does not serve the audio operations")
			return
		}
		unsupportedOperation(c, "audio operations require an OpenAI-compatible channel")
		return
	}
	switch {
	case strings.HasSuffix(c.Request.URL.Path, "/audio/speech"):
		NativeOpenAIAudioSpeech(c)
	case strings.HasSuffix(c.Request.URL.Path, "/audio/transcriptions"):
		NativeOpenAIAudioTranscriptions(c)
	default:
		NativeOpenAIAudioTranslations(c)
	}
}

func RelayNotFound(c *gin.Context) {
	err := model.Error{
		Message: formatInvalidURL(c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func formatInvalidURL(method, path string) string {
	return "Invalid URL (" + method + " " + path + ")"
}
