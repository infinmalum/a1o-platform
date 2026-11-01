package bridge

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// providerMetadata is the opaque carry channel for upstream envelope fields a
// target wire cannot represent natively. Every conversion that drops nothing
// routes leftover upstream fields through here instead of deleting them, and
// clients round-trip the object back inside request extension slots where the
// paired direction needs it.
type providerMetadata struct {
	fields map[string]any
}

func newProviderMetadata(extras map[string]json.RawMessage) *providerMetadata {
	if len(extras) == 0 {
		return &providerMetadata{}
	}
	fields := make(map[string]any, len(extras))
	for key, raw := range extras {
		fields[key] = json.RawMessage(raw)
	}
	return &providerMetadata{fields: fields}
}

// set stores one value under the metadata key, creating the slot on demand.
func (p *providerMetadata) set(key string, value any) {
	if p.fields == nil {
		p.fields = make(map[string]any, 2)
	}
	p.fields[key] = value
}

// value returns the carried fields, or nil when nothing was collected so
// empty metadata objects never reach the wire.
func (p *providerMetadata) value() map[string]any {
	if len(p.fields) == 0 {
		return nil
	}
	return p.fields
}

// envelope returns {"provider_metadata": {...}} when anything was carried.
func (p *providerMetadata) envelope() map[string]any {
	value := p.value()
	if value == nil {
		return nil
	}
	return map[string]any{"provider_metadata": value}
}

// absorb merges raw extras collected from another response level.
func (p *providerMetadata) absorb(extras map[string]json.RawMessage) {
	for key, raw := range extras {
		p.set(key, json.RawMessage(raw))
	}
}

// streamExtras accumulates unparsed upstream stream fields until the
// converter's terminal event, where they are emitted as provider_metadata.
type streamExtras struct {
	metadata providerMetadata
}

func (s *streamExtras) absorb(extras map[string]json.RawMessage) {
	s.metadata.absorb(extras)
}

func (s *streamExtras) set(key string, value any) {
	s.metadata.set(key, value)
}

// envelope returns {"provider_metadata": {...}} when anything was carried.
func (s *streamExtras) envelope() map[string]any {
	return s.metadata.envelope()
}

// chatUsageEnvelope renders the Chat wire's usage object including the
// cached-token and reasoning-token details when the provider reported them.
func chatUsageEnvelope(usage native.Usage) map[string]any {
	result := map[string]any{
		"prompt_tokens":     usage.Input,
		"completion_tokens": usage.Output,
		"total_tokens":      usage.Input + usage.Output,
	}
	if usage.CacheRead > 0 {
		result["prompt_tokens_details"] = map[string]int64{"cached_tokens": usage.CacheRead}
	}
	if usage.Reasoning > 0 {
		result["completion_tokens_details"] = map[string]int64{"reasoning_tokens": usage.Reasoning}
	}
	return result
}

// responsesUsageEnvelope renders the Responses wire usage object with the
// cached-token and reasoning-token details when reported.
func responsesUsageEnvelope(usage native.Usage) map[string]any {
	result := map[string]any{
		"input_tokens":  usage.Input,
		"output_tokens": usage.Output,
		"total_tokens":  usage.Input + usage.Output,
	}
	if usage.CacheRead > 0 {
		result["input_tokens_details"] = map[string]int64{"cached_tokens": usage.CacheRead}
	}
	if usage.Reasoning > 0 {
		result["output_tokens_details"] = map[string]int64{"reasoning_tokens": usage.Reasoning}
	}
	return result
}

// anthropicUsageEnvelope renders the Anthropic wire usage object. input_tokens
// excludes cache tokens per Anthropic semantics; the cache breakdown travels
// in its own fields so the client sees the provider's real numbers.
func anthropicUsageEnvelope(usage native.Usage) map[string]any {
	result := map[string]any{
		"input_tokens":  usage.Input - usage.CacheRead - usage.CacheCreation,
		"output_tokens": usage.Output,
	}
	if usage.CacheCreation > 0 {
		result["cache_creation_input_tokens"] = usage.CacheCreation
	}
	if usage.CacheRead > 0 {
		result["cache_read_input_tokens"] = usage.CacheRead
	}
	return result
}

// sortExtrasNames exists for tests and error messages that list extra keys in
// a stable order.
func sortExtrasNames(extras map[string]json.RawMessage) string {
	names := make([]string, 0, len(extras))
	for key := range extras {
		names = append(names, key)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
