package lifecycle

import (
	"testing"

	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
)

// seedTestModelPrices prices the subscription fixtures' model in the global
// price book, which ships empty: a model bills only what the operator or an
// upstream sync priced. The previous book is restored when the test finishes.
func seedTestModelPrices(t *testing.T) {
	t.Helper()
	previousInput, previousCompletion := billingratio.ModelRatio2JSONString(), billingratio.CompletionRatio2JSONString()
	t.Cleanup(func() {
		_ = billingratio.UpdateModelRatioByJSONString(previousInput)
		_ = billingratio.UpdateCompletionRatioByJSONString(previousCompletion)
	})
	if err := billingratio.UpdateModelRatioByJSONString(`{"gpt-5": 2, "gpt-5-mapped": 2}`); err != nil {
		t.Fatal(err)
	}
	if err := billingratio.UpdateCompletionRatioByJSONString(`{"gpt-5": 3, "gpt-5-mapped": 3}`); err != nil {
		t.Fatal(err)
	}
}
