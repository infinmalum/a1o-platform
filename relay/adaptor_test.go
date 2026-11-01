package relay

import (
	"testing"

	"github.com/infinmalum/a1o-platform/relay/apitype"
)

func TestGetAdaptor(t *testing.T) {
	for i := 0; i < apitype.Dummy; i++ {
		if i == apitype.SystemOne || i == apitype.Dummy {
			// SystemOne channels only serve the native /v1/systemone route,
			// so they have no legacy adapter. The sentinel type has none
			// either.
			continue
		}
		if i == apitype.OpenAI || i == apitype.Anthropic || i == apitype.OpenAIResponses || i == apitype.OpenResponses {
			// The OpenAI, Anthropic, and Responses wires are served by the
			// native lifecycle, not a legacy adapter.
			continue
		}
		if i == apitype.AwsClaude || i == apitype.Cloudflare || i == apitype.VertexAI {
			// The Bedrock, Cloudflare, and VertexAI kinds run their native
			// protocol wires through the native layer's provider transports;
			// their conversion adapters are gone.
			continue
		}
		if GetAdaptor(i) == nil {
			t.Errorf("api type %d has no adaptor", i)
		}
	}
}
