package relay

import (
	"testing"

	"github.com/infinmalum/one-gateway/relay/apitype"
)

// retiredAPITypes are the wire protocols whose per-vendor adapters were
// removed: every vendor now publishes an official OpenAI-compatible endpoint,
// so those channel types ride the shared OpenAI adapter instead. The
// constants stay in the iota table for numbering stability only.
var retiredAPITypes = map[int]bool{
	apitype.PaLM:          true,
	apitype.Baidu:         true,
	apitype.Zhipu:         true,
	apitype.Ali:           true,
	apitype.Xunfei:        true,
	apitype.AIProxyLibrary: true,
	apitype.Tencent:       true,
	apitype.Cohere:        true,
	apitype.Ollama:        true,
}

func TestGetAdaptor(t *testing.T) {
	for i := 0; i < apitype.Dummy; i++ {
		if i == apitype.Proxy || i == apitype.SystemOne || i == apitype.Dummy {
			// Proxy requests bypass adapters entirely; the sentinel type has
			// no adaptor. SystemOne channels only serve the native
			// /v1/systemone route, so they have no legacy adapter either.
			continue
		}
		if retiredAPITypes[i] {
			// Retired wire protocols have no adapter of their own; their
			// channel types map onto apitype.OpenAI via ToAPIType.
			continue
		}
		if i == apitype.OpenAIResponses {
			// The Responses wire is bridged by the shared lifecycle, not a
			// legacy adapter.
			continue
		}
		if GetAdaptor(i) == nil {
			t.Errorf("api type %d has no adaptor", i)
		}
	}
}
