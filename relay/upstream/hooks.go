package upstream

import (
	"fmt"
	"sync"

	"github.com/infinmalum/a1o-platform/relay/credential"
)

// RequestValidator enforces a profile's capability matrix on one upstream
// request body — the converted body when a bridge produced it — and returns
// the normalized body to forward. An error fails the attempt explicitly; the
// lifecycle decides whether another channel may serve.
type RequestValidator func(body []byte) ([]byte, error)

var (
	hooksMu    sync.RWMutex
	refreshers = map[string]credential.Refresher{}
	validators = map[string]RequestValidator{}
)

// RegisterRefresher wires the credential rotator of one profile, for example
// the OAuth refresh of chatgpt_plan_public.
func RegisterRefresher(profileID string, refresher credential.Refresher) error {
	if profileID == "" {
		return fmt.Errorf("profile ID must not be empty")
	}
	if refresher == nil {
		return fmt.Errorf("refresher must not be nil")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	refreshers[profileID] = refresher
	return nil
}

// RefresherFor returns the registered credential rotator of a profile.
func RefresherFor(profileID string) (credential.Refresher, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	refresher, ok := refreshers[profileID]
	return refresher, ok
}

// RegisterRequestValidator wires the capability validator of one profile.
func RegisterRequestValidator(profileID string, validator RequestValidator) error {
	if profileID == "" {
		return fmt.Errorf("profile ID must not be empty")
	}
	if validator == nil {
		return fmt.Errorf("validator must not be nil")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	validators[profileID] = validator
	return nil
}

// RequestValidatorFor returns the registered capability validator of a profile.
func RequestValidatorFor(profileID string) (RequestValidator, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	validator, ok := validators[profileID]
	return validator, ok
}
