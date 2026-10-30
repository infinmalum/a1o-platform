package chat

import (
	"testing"

	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
)

// seedTestModelPrices prices the integration fixture's model in the global
// price book, which ships empty: a model bills only what the operator or an
// upstream sync priced. The previous book is restored when the test finishes.
func seedTestModelPrices(t *testing.T) {
	t.Helper()
	previousInput, previousCompletion := billingratio.ModelRatio2JSONString(), billingratio.CompletionRatio2JSONString()
	t.Cleanup(func() {
		_ = billingratio.UpdateModelRatioByJSONString(previousInput)
		_ = billingratio.UpdateCompletionRatioByJSONString(previousCompletion)
	})
	if err := billingratio.UpdateModelRatioByJSONString(`{"test-model": 2}`); err != nil {
		t.Fatal(err)
	}
	if err := billingratio.UpdateCompletionRatioByJSONString(`{"test-model": 3}`); err != nil {
		t.Fatal(err)
	}
}
