package lifecycle

import (
	"testing"

	"github.com/infinmalum/a1o-platform/relay"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/bridge"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// TestThreeEndpointKindCoverage locks the full-coverage matrix the three
// client endpoints promise: every channel kind except TypeSafe and DeepL (the
// decision and translation wires) serves Chat, Responses, and Messages
// clients — native kinds through the lifecycle's protocol support and the
// bridge's direct conversions, provider kinds through their adapters. When a
// row fails, the endpoint that kind lost is the bug.
func TestThreeEndpointKindCoverage(t *testing.T) {
	chatCompatible := []struct {
		kind     int
		upstream native.Protocol
	}{
		{channeltype.OpenAI, native.OpenAIChat},
		{channeltype.Azure, native.OpenAIChat},
		{channeltype.Cloudflare, native.OpenAIChat},
		{channeltype.OpenRouter, native.OpenAIChat},
		{channeltype.Anthropic, native.Anthropic},
		{channeltype.AwsClaude, native.Anthropic},
		{channeltype.VertextAI, native.Anthropic},
		{channeltype.VertextAI, native.Gemini},
		{channeltype.Gemini, native.Gemini},
		{channeltype.OpenAIResponses, native.OpenAIResponses},
		{channeltype.Vercel, native.OpenAIResponses},
	}
	providerKinds := []int{channeltype.Coze, channeltype.Replicate}

	inbounds := []native.Protocol{native.OpenAIChat, native.OpenAIResponses, native.Anthropic}
	for _, inbound := range inbounds {
		for _, row := range chatCompatible {
			if !channelSupportsProtocol(row.kind, row.upstream) {
				t.Errorf("kind %d no longer speaks %s: endpoint %s coverage broke", row.kind, row.upstream, inbound)
				continue
			}
			converter, err := bridge.For(inbound, row.upstream)
			if err != nil {
				t.Errorf("endpoint %s onto kind %d (%s upstream) has no conversion: %v", inbound, row.kind, row.upstream, err)
			}
			if converter != nil && inbound == row.upstream {
				t.Errorf("equal protocols must pass through without a converter: %s", inbound)
			}
		}
		for _, kind := range providerKinds {
			if GetAdaptorForKind(kind) == nil {
				t.Errorf("endpoint %s onto provider kind %d has no adapter", inbound, kind)
			}
		}
	}

	// TypeSafe and DeepL are the deliberate exclusions: the decision wire
	// only serves /v1/systemone and the translation wire only its own route.
	for _, kind := range []int{channeltype.TypeSafe, channeltype.DeepL} {
		for _, upstream := range []native.Protocol{native.OpenAIChat, native.OpenAIResponses, native.Anthropic, native.Gemini} {
			if channelSupportsProtocol(kind, upstream) {
				t.Errorf("kind %d must not serve the %s wire", kind, upstream)
			}
		}
	}

	// The candidate lists the unpinned selections query must agree with the
	// per-kind support table, except the Responses kinds that serve Chat
	// clients through the chat-to-Responses bridge (their own wire is
	// Responses, so a negotiated Chat upstream never lands on them).
	bridgedChatKinds := map[int]bool{channeltype.OpenAIResponses: true, channeltype.Vercel: true}
	for _, upstream := range []native.Protocol{native.OpenAIChat, native.OpenAIResponses, native.Anthropic, native.Gemini} {
		for _, kind := range candidateChannelTypes(upstream) {
			if channelSupportsProtocol(kind, upstream) {
				continue
			}
			if upstream == native.OpenAIChat && bridgedChatKinds[kind] && channelSupportsProtocol(kind, native.OpenAIResponses) {
				continue
			}
			t.Errorf("candidate list for %s contains kind %d that neither supports it nor bridges onto its own wire", upstream, kind)
		}
	}
}

// GetAdaptorForKind reports the provider adapter for a channel kind, the seam
// the native routes dispatch to for the provider kinds.
func GetAdaptorForKind(kind int) adaptor.Adaptor {
	return relay.GetAdaptor(channeltype.ToAPIType(kind))
}
