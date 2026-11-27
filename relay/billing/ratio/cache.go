package ratio

import (
	"encoding/json"
	"fmt"
)

// CachePrices holds optional absolute rates in the same units as ModelRatio.
// A missing rate follows the input price. An explicit zero is free.
type CachePrices struct {
	Read    *float64 `json:"read,omitempty"`
	Write   *float64 `json:"write,omitempty"`
	Write1h *float64 `json:"write_1h,omitempty"`
}

// The cache prices share the input price book's lock.
var modelCachePrices = map[string]CachePrices{}

func LookupCachePrices(model string) CachePrices {
	modelRatioLock.RLock()
	defer modelRatioLock.RUnlock()
	return modelCachePrices[model]
}

func ModelCachePricesJSON() string {
	modelRatioLock.RLock()
	defer modelRatioLock.RUnlock()
	raw, _ := json.Marshal(modelCachePrices)
	return string(raw)
}

func UpdateModelCachePrices(raw string) error {
	var prices map[string]CachePrices
	if err := json.Unmarshal([]byte(raw), &prices); err != nil {
		return err
	}
	for model, price := range prices {
		if (price.Read != nil && *price.Read < 0) || (price.Write != nil && *price.Write < 0) || (price.Write1h != nil && *price.Write1h < 0) {
			return fmt.Errorf("cache prices for %s must be non-negative", model)
		}
	}
	modelRatioLock.Lock()
	defer modelRatioLock.Unlock()
	if prices == nil {
		prices = make(map[string]CachePrices)
	}
	modelCachePrices = prices
	return nil
}
