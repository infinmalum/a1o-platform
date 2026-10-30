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
