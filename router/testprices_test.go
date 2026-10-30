package router

import (
	"encoding/json"
	"testing"

	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
)

// The router fixtures bill through the real price book, which ships empty by
// design: a model bills only what the operator or an upstream sync priced.
// Each billing test seeds the union list of its fixture models and restores
// the previous book when it finishes. Image and audio fixtures are priced
// input-only because their charges are fixed amounts without an output
// component; chat fixtures carry a completion multiplier because billing
// refuses models missing either price.
var fixtureInputPrices = map[string]float64{
	// native route fixtures
	"claude-upstream": 2, "gemini-upstream": 2, "future-model-id": 2,
	"openai-only": 2, "blocked-model": 2, "chat-upstream": 2,
	"bridge-upstream": 2, "gemini-bridge-upstream": 2, "reverse-upstream": 2,
	"completion-upstream": 2, "embedding-upstream": 2, "moderation-upstream": 2,
	// official SDK fixtures
	"upstream-openai": 2, "upstream-anthropic": 2, "upstream-ollama": 2,
	"gemini-tool-upstream": 2, "gemini-think-upstream": 2,
	"gemini-stream-upstream": 2, "future-gemini-model": 2,
	"responses-upstream": 2, "phase5-gpt": 2, "upstream-ollama-embed": 2,
	// stream conversion fixtures
	"claude-streamed": 2, "gemini-streamed": 2, "chat-streamed": 2, "gemini-sync": 2,
	// legacy image, audio, and chat fixtures
	"legacy-chat": 2, "legacy-completion": 2, "legacy-embed": 2,
	"legacy-moderation": 2, "retry-image": 2, "alias": 2, "legacy-failure": 2, "redirect-only": 2,
	"dall-e-2": 10, "dall-e-3": 20, "tts-1": 7.5, "whisper-1": 15,
	"phase5-tts": 7.5, "upstream-cogview": 7.5,
}

var fixtureCompletionPrices = map[string]float64{
	"claude-upstream": 3, "gemini-upstream": 3, "future-model-id": 3,
	"openai-only": 3, "blocked-model": 3, "chat-upstream": 3,
	"bridge-upstream": 3, "gemini-bridge-upstream": 3, "reverse-upstream": 3,
	"completion-upstream": 3, "embedding-upstream": 3, "moderation-upstream": 3,
	"upstream-openai": 3, "upstream-anthropic": 3, "upstream-ollama": 3,
	"gemini-tool-upstream": 3, "gemini-think-upstream": 3,
	"gemini-stream-upstream": 3, "future-gemini-model": 3,
	"responses-upstream": 3, "phase5-gpt": 3, "upstream-ollama-embed": 3,
	"claude-streamed": 3, "gemini-streamed": 3, "chat-streamed": 3, "gemini-sync": 3,
	"legacy-chat": 3, "legacy-completion": 3, "legacy-embed": 3, "legacy-moderation": 3, "alias": 3, "legacy-failure": 3, "redirect-only": 3,
}

func seedFixturePrices(t *testing.T) {
	t.Helper()
	previousInput, previousCompletion := billingratio.ModelRatio2JSONString(), billingratio.CompletionRatio2JSONString()
	t.Cleanup(func() {
		_ = billingratio.UpdateModelRatioByJSONString(previousInput)
		_ = billingratio.UpdateCompletionRatioByJSONString(previousCompletion)
	})
	var inputBook, completionBook map[string]float64
	if err := json.Unmarshal([]byte(previousInput), &inputBook); err != nil {
		t.Fatalf("existing model ratio book: %v", err)
	}
	if err := json.Unmarshal([]byte(previousCompletion), &completionBook); err != nil {
		t.Fatalf("existing completion ratio book: %v", err)
	}
	for name, price := range fixtureInputPrices {
		inputBook[name] = price
	}
	for name, price := range fixtureCompletionPrices {
		completionBook[name] = price
	}
	inputJSON, err := json.Marshal(inputBook)
	if err != nil {
		t.Fatal(err)
	}
	completionJSON, err := json.Marshal(completionBook)
	if err != nil {
		t.Fatal(err)
	}
	if err := billingratio.UpdateModelRatioByJSONString(string(inputJSON)); err != nil {
		t.Fatal(err)
	}
	if err := billingratio.UpdateCompletionRatioByJSONString(string(completionJSON)); err != nil {
		t.Fatal(err)
	}
}

func mustModelRatio(t *testing.T, name string) float64 {
	t.Helper()
	ratio, err := billingratio.GetModelRatio(name)
	if err != nil {
		t.Fatalf("price %s: %v", name, err)
	}
	return ratio
}

func mustCompletionRatio(t *testing.T, name string) float64 {
	t.Helper()
	ratio, err := billingratio.GetCompletionRatio(name)
	if err != nil {
		t.Fatalf("completion price %s: %v", name, err)
	}
	return ratio
}
