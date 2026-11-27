package ratio

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/infinmalum/a1o-platform/common/logger"
)

const (
	USD = 500 // $0.002 = 1 -> $1 = 500
)

var modelRatioLock sync.RWMutex
var completionRatioLock sync.RWMutex

// ModelRatio is the operator's price book: input prices in ratio units,
// where 1 = $0.002 per 1K tokens = $2 per million. The gateway ships no
// vendor price dictionary — a model bills only when an entry exists here,
// filled either by hand in the console price editor or from an upstream
// that publishes prices (OpenRouter's model catalog). Unpriced models are
// refused, never billed at an invented rate.
var ModelRatio = map[string]float64{}

// CompletionRatio holds output prices as output-over-input multipliers. A
// zero entry bills no output component, which is what per-character and
// transcription charges need: their usage reports carry no output tokens.
var CompletionRatio = map[string]float64{}

// contractRatio seeds the first-party integrations whose prices are part of
// the vendor contract itself, so a fresh install serves them without a
// manual pricing step:
//   - TypeSafe Jev bills $0.042 per million input tokens and never charges
//     for output.
//
// Everything else is priced by the operator or synced from the upstream.
func contractRatio(name string) (input float64, completion float64, ok bool) {
	switch name {
	case "jev-latest", "jev-1.13.0", "jev-preview":
		return 0.042 / 1000 * USD, 0, true
	}
	return 0, 0, false
}

// GetModelRatio resolves the input price for billing. An unpriced model is
// an error: the gateway refuses to invent a charge for it.
func GetModelRatio(name string) (float64, error) {
	if ratio, ok := LookupModelRatio(name); ok {
		return ratio, nil
	}
	return 0, fmt.Errorf("模型 %s 未配置价格：请在管理台「运营设置 → 模型价格」填写，或对发布价格的渠道（如 OpenRouter）点「获取模型」同步", name)
}

// GetCompletionRatio resolves the output multiplier for billing. An
// unpriced output is an error for the same reason as an unpriced input.
func GetCompletionRatio(name string) (float64, error) {
	if ratio, ok := LookupCompletionRatio(name); ok {
		return ratio, nil
	}
	return 0, fmt.Errorf("模型 %s 未配置输出价格：请在管理台「运营设置 → 模型价格」填写输出价，或对发布价格的渠道（如 OpenRouter）点「获取模型」同步", name)
}

// LookupModelRatio resolves a configured input price and reports whether one
// exists. Price listings ask about every reachable model and show unpriced
// ones as unpriced instead of failing.
func LookupModelRatio(name string) (float64, bool) {
	modelRatioLock.RLock()
	defer modelRatioLock.RUnlock()
	if ratio, ok := ModelRatio[name]; ok {
		return ratio, true
	}
	input, _, ok := contractRatio(name)
	return input, ok
}

// LookupCompletionRatio resolves a configured output multiplier and reports
// whether one exists.
func LookupCompletionRatio(name string) (float64, bool) {
	completionRatioLock.RLock()
	defer completionRatioLock.RUnlock()
	if ratio, ok := CompletionRatio[name]; ok {
		return ratio, true
	}
	_, completion, ok := contractRatio(name)
	return completion, ok
}

// FetchedModelPrice is one upstream-published price in ratio units: the
// input price and the output-over-input multiplier.
type FetchedModelPrice struct {
	Input      float64
	Completion float64
	Cache      CachePrices
}

// MergeFetchedModelPrices fills the price book with upstream-published
// prices for new models and missing cache prices for existing models. Existing
// prices are preserved. It returns the number of models with new prices. A model with free input but paid output is
// skipped: the charge formula multiplies the whole bill by the input ratio,
// so such a price cannot be expressed.
func MergeFetchedModelPrices(prices map[string]FetchedModelPrice) int {
	modelRatioLock.Lock()
	completionRatioLock.Lock()
	defer modelRatioLock.Unlock()
	defer completionRatioLock.Unlock()
	added := 0
	for name, price := range prices {
		if _, _, seeded := contractRatio(name); seeded {
			continue
		}
		if price.Input < 0 || price.Completion < 0 {
			continue
		}
		if price.Input == 0 && price.Completion > 0 {
			continue
		}
		_, exists := ModelRatio[name]
		if !exists {
			ModelRatio[name] = price.Input
			CompletionRatio[name] = price.Completion
		}
		cache := modelCachePrices[name]
		changed := !exists
		if cache.Read == nil && price.Cache.Read != nil {
			cache.Read, changed = price.Cache.Read, true
		}
		if cache.Write == nil && price.Cache.Write != nil {
			cache.Write, changed = price.Cache.Write, true
		}
		if cache.Write1h == nil && price.Cache.Write1h != nil {
			cache.Write1h, changed = price.Cache.Write1h, true
		}
		if changed {
			if cache.Read != nil || cache.Write != nil || cache.Write1h != nil {
				modelCachePrices[name] = cache
			}
			added++
		}
	}
	return added
}

func ModelRatio2JSONString() string {
	modelRatioLock.RLock()
	defer modelRatioLock.RUnlock()
	jsonBytes, err := json.Marshal(ModelRatio)
	if err != nil {
		logger.SysError("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func CompletionRatio2JSONString() string {
	completionRatioLock.RLock()
	defer completionRatioLock.RUnlock()
	jsonBytes, err := json.Marshal(CompletionRatio)
	if err != nil {
		logger.SysError("error marshalling completion ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRatioByJSONString(jsonStr string) error {
	modelRatioLock.Lock()
	defer modelRatioLock.Unlock()
	ModelRatio = make(map[string]float64)
	return json.Unmarshal([]byte(jsonStr), &ModelRatio)
}

func UpdateCompletionRatioByJSONString(jsonStr string) error {
	completionRatioLock.Lock()
	defer completionRatioLock.Unlock()
	CompletionRatio = make(map[string]float64)
	return json.Unmarshal([]byte(jsonStr), &CompletionRatio)
}

// ModelRatioNames returns the configured model names for read-only iteration.
// Ranging over ModelRatio directly is a data race: option updates swap the
// map concurrently with requests.
func ModelRatioNames() []string {
	modelRatioLock.RLock()
	defer modelRatioLock.RUnlock()
	names := make([]string, 0, len(ModelRatio))
	for name := range ModelRatio {
		names = append(names, name)
	}
	return names
}
