// Package upstream describes transport profiles: how a request reaches a
// provider and with which credential kind. The profile registry is data plus
// per-profile hooks (credential refreshers and request validators); request
// execution stays owned by the relay lifecycle. Unbound channels use the
// builtin api_key profile, which is the compatibility path for every existing
// API-key channel.
package upstream

import (
	"fmt"
	"sort"
	"sync"

	"github.com/infinmalum/a1o-platform/relay/native"
)

type Profile struct {
	ID       string // for example "api_key" or "chatgpt_plan_public"
	Provider string // for example "openai"; empty for the generic API-key profile
	// CredentialKind selects the credential document shape, matching
	// relay/credential kinds.
	CredentialKind string
	// UpstreamProtocols lists the native protocols this profile can serve.
	UpstreamProtocols []native.Protocol
	// Subscription marks profiles backed by administrator-connected accounts
	// rather than per-channel API keys.
	Subscription bool
}

var (
	mu       sync.RWMutex
	profiles = map[string]Profile{
		"api_key": {
			ID:               "api_key",
			CredentialKind:   "api_key",
			UpstreamProtocols: []native.Protocol{
				native.Anthropic, native.Gemini, native.OpenAIChat, native.OpenAICompletions,
				native.OpenAIEmbeddings, native.OpenAIModerations, native.OpenAIResponses,
				native.OpenAIImages, native.OpenAIAudioSpeech,
				native.OpenAIAudioTranscriptions, native.OpenAIAudioTranslations,
			},
		},
		// First subscription profile: a ChatGPT plan account connected through
		// Sign in with ChatGPT, served over the public Responses endpoint. The
		// transport itself lands with the S2 implementation.
		"chatgpt_plan_public": {
			ID:                "chatgpt_plan_public",
			Provider:          "openai",
			CredentialKind:    "oauth",
			UpstreamProtocols: []native.Protocol{native.OpenAIResponses},
			Subscription:      true,
		},
	}
)

// Register adds or replaces a profile descriptor.
func Register(profile Profile) error {
	if profile.ID == "" {
		return fmt.Errorf("profile ID must not be empty")
	}
	mu.Lock()
	defer mu.Unlock()
	profiles[profile.ID] = profile
	return nil
}

func Lookup(id string) (Profile, bool) {
	mu.RLock()
	defer mu.RUnlock()
	profile, ok := profiles[id]
	return profile, ok
}

func All() []Profile {
	mu.RLock()
	defer mu.RUnlock()
	list := make([]Profile, 0, len(profiles))
	for _, profile := range profiles {
		list = append(list, profile)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}
