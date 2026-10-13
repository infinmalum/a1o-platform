package account

import (
	"strconv"
	"sync"
	"time"

	"github.com/infinmalum/one-gateway/common/logger"
	"github.com/infinmalum/one-gateway/model"
)

// Observation is one cooldown record with its account/model scope. An empty
// Model cools down the whole account.
type Observation struct {
	AccountID int
	Model     string
	Reason    string
	Until     time.Time
}

// CooldownRegistry keeps the hot-path view of active cooldowns and reports
// applied observations to an optional persistence hook.
type CooldownRegistry struct {
	mu      sync.Mutex
	until   map[string]time.Time
	persist func(Observation)
}

func NewCooldownRegistry(persist func(Observation)) *CooldownRegistry {
	return &CooldownRegistry{
		until:   make(map[string]time.Time),
		persist: persist,
	}
}

func cooldownKey(accountID int, modelName string) string {
	return strconv.Itoa(accountID) + "\x00" + modelName
}

// Set records a cooldown when it extends the active one for the same scope.
func (r *CooldownRegistry) Set(observation Observation) {
	if observation.Until.IsZero() {
		return
	}
	r.mu.Lock()
	key := cooldownKey(observation.AccountID, observation.Model)
	if until, ok := r.until[key]; ok && !until.Before(observation.Until) {
		r.mu.Unlock()
		return
	}
	r.until[key] = observation.Until
	r.mu.Unlock()
	if r.persist != nil {
		r.persist(observation)
	}
}

// Active reports whether the account, or the requested model on that account,
// is under an unexpired cooldown. Expired entries are dropped lazily.
func (r *CooldownRegistry) Active(accountID int, modelName string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, key := range []string{cooldownKey(accountID, ""), cooldownKey(accountID, modelName)} {
		if until, ok := r.until[key]; ok {
			if now.Before(until) {
				return true
			}
			delete(r.until, key)
		}
	}
	return false
}

// Remaining returns how long the stricter of the account-wide and model
// cooldowns still runs; zero means no active cooldown.
func (r *CooldownRegistry) Remaining(accountID int, modelName string) time.Duration {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var remaining time.Duration
	for _, key := range []string{cooldownKey(accountID, ""), cooldownKey(accountID, modelName)} {
		if until, ok := r.until[key]; ok {
			if left := until.Sub(now); left > remaining {
				remaining = left
			}
		}
	}
	return remaining
}

func persistCooldown(observation Observation) {
	if err := model.InsertUpstreamAccountCooldown(observation.AccountID, observation.Model, observation.Reason, observation.Until.Unix()); err != nil {
		logger.SysError("failed to persist upstream account cooldown: " + err.Error())
	}
}

// DefaultCooldowns is the process-wide registry with database persistence.
var DefaultCooldowns = NewCooldownRegistry(persistCooldown)
