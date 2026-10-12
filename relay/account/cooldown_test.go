package account

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCooldownScopeAndExpiry(t *testing.T) {
	registry := NewCooldownRegistry(nil)

	require.False(t, registry.Active(1, "gpt-5"))

	// Model-scoped cooldown blocks that model and nothing else.
	registry.Set(Observation{AccountID: 1, Model: "gpt-5", Reason: "429", Until: time.Now().Add(time.Minute)})
	require.True(t, registry.Active(1, "gpt-5"))
	require.False(t, registry.Active(1, "gpt-5-mini"))
	require.False(t, registry.Active(2, "gpt-5"))

	// Account-wide cooldown blocks every model on that account.
	registry.Set(Observation{AccountID: 2, Reason: "reauth", Until: time.Now().Add(time.Minute)})
	require.True(t, registry.Active(2, "gpt-5"))
	require.True(t, registry.Active(2, "anything"))

	// A model cooldown inside an account-wide cooldown is the stricter one.
	require.Equal(t, time.Minute, registry.Remaining(2, "gpt-5").Round(time.Minute))
}

func TestCooldownKeepsLongerWindow(t *testing.T) {
	registry := NewCooldownRegistry(nil)
	observations := []Observation{}
	persist := func(o Observation) { observations = append(observations, o) }
	registry.persist = persist

	short := Observation{AccountID: 1, Model: "gpt-5", Reason: "429", Until: time.Now().Add(time.Minute)}
	long := Observation{AccountID: 1, Model: "gpt-5", Reason: "529", Until: time.Now().Add(10 * time.Minute)}
	registry.Set(short)
	registry.Set(long)
	require.Equal(t, 10*time.Minute, registry.Remaining(1, "gpt-5").Round(time.Minute))
	// Both applications were persisted for observability.
	require.Len(t, observations, 2)

	// A shorter follow-up does not shorten the active window and is not persisted.
	registry.Set(short)
	require.Len(t, observations, 2)
	require.Equal(t, 10*time.Minute, registry.Remaining(1, "gpt-5").Round(time.Minute))
}

func TestCooldownExpires(t *testing.T) {
	registry := NewCooldownRegistry(nil)
	registry.Set(Observation{AccountID: 1, Model: "gpt-5", Reason: "429", Until: time.Now().Add(20 * time.Millisecond)})
	require.True(t, registry.Active(1, "gpt-5"))
	time.Sleep(30 * time.Millisecond)
	require.False(t, registry.Active(1, "gpt-5"))
	require.Equal(t, time.Duration(0), registry.Remaining(1, "gpt-5"))
}

func TestCooldownIgnoresZeroUntil(t *testing.T) {
	registry := NewCooldownRegistry(nil)
	registry.Set(Observation{AccountID: 1, Model: "gpt-5", Reason: "429"})
	require.False(t, registry.Active(1, "gpt-5"))
}

func TestNilCooldownRegistryIsInactive(t *testing.T) {
	var registry *CooldownRegistry
	require.False(t, registry.Active(1, "gpt-5"))
	require.Equal(t, time.Duration(0), registry.Remaining(1, "gpt-5"))
}
