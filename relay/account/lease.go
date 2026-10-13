// Package account coordinates subscription account usage: in-flight leases
// with bounded waits and cooldown state. Coordination is process-local; the
// first deployment supports one gateway replica.
package account

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/infinmalum/one-gateway/common/config"
)

var (
	// ErrCapacity reports a full account or a full global subscription budget
	// after waiting up to the caller's bound.
	ErrCapacity = errors.New("account capacity is full")
	// ErrManagerDisabled reports calls on an unconfigured manager.
	ErrManagerDisabled = errors.New("account manager is not configured")
)

// globalWaiterKey separates global-cap waiters from per-account waiters.
// Account IDs are positive, so there is no collision.
const globalWaiterKey = -1

type Manager struct {
	mu          sync.Mutex
	held        map[int]int
	waiters     map[int][]chan struct{}
	totalHeld   int
	totalWaiters []chan struct{}
	totalCap    int
}

// NewManager creates a lease manager. totalCap <= 0 means the total
// subscription in-flight budget is unlimited.
func NewManager(totalCap int) *Manager {
	return &Manager{
		held:     make(map[int]int),
		waiters:  make(map[int][]chan struct{}),
		totalCap: totalCap,
	}
}

// Acquire reserves one in-flight slot for the account. limit <= 0 means the
// account has no per-account limit. maxWait bounds how long a full account or
// a full global budget may be waited for; maxWait <= 0 fails immediately
// instead of waiting.
func (m *Manager) Acquire(ctx context.Context, accountID int, limit int, maxWait time.Duration) (*Lease, error) {
	if m == nil {
		return nil, ErrManagerDisabled
	}
	if limit < 0 {
		limit = 0
	}
	for {
		waitCh, ok := m.tryAcquire(accountID, limit)
		if ok {
			return &Lease{manager: m, accountID: accountID}, nil
		}
		if err := m.waitForWake(ctx, accountID, waitCh, maxWait); err != nil {
			return nil, err
		}
	}
}

// tryAcquire takes a slot or registers a waiter channel. A waiter blocked by
// the account limit waits on its account key; one blocked by the global budget
// waits on the global key, so releases wake exactly the right set.
func (m *Manager) tryAcquire(accountID int, limit int) (chan struct{}, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.totalCap > 0 && m.totalHeld >= m.totalCap {
		ch := make(chan struct{})
		m.totalWaiters = append(m.totalWaiters, ch)
		return ch, false
	}
	if limit > 0 && m.held[accountID] >= limit {
		ch := make(chan struct{})
		m.waiters[accountID] = append(m.waiters[accountID], ch)
		return ch, false
	}
	m.held[accountID]++
	m.totalHeld++
	return nil, true
}

func (m *Manager) waitForWake(ctx context.Context, accountID int, ch chan struct{}, maxWait time.Duration) error {
	if maxWait <= 0 {
		// Callers that do not allow waiting fail fast instead of parking.
		m.removeWaiter(accountID, ch)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrCapacity
	}
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		m.removeWaiter(accountID, ch)
		return ctx.Err()
	case <-timer.C:
		m.removeWaiter(accountID, ch)
		return ErrCapacity
	}
}

// removeWaiter drops an abandoned waiter so releases do not close a channel
// nobody is listening on.
func (m *Manager) removeWaiter(accountID int, ch chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waiters[accountID] = removeChannel(m.waiters[accountID], ch)
	m.totalWaiters = removeChannel(m.totalWaiters, ch)
}

func removeChannel(channels []chan struct{}, target chan struct{}) []chan struct{} {
	for i, ch := range channels {
		if ch == target {
			return append(channels[:i], channels[i+1:]...)
		}
	}
	return channels
}

func (m *Manager) release(accountID int) {
	m.mu.Lock()
	if m.held[accountID] > 0 {
		m.held[accountID]--
	}
	if m.totalHeld > 0 {
		m.totalHeld--
	}
	waiters := m.waiters[accountID]
	m.waiters[accountID] = nil
	total := m.totalWaiters
	m.totalWaiters = nil
	m.mu.Unlock()
	// Woken waiters re-check the counters under the lock; a spurious wake
	// simply re-registers.
	for _, ch := range waiters {
		close(ch)
	}
	for _, ch := range total {
		close(ch)
	}
}

// Held reports the current in-flight count of one account.
func (m *Manager) Held(accountID int) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.held[accountID]
}

// Lease is one acquired in-flight slot. Release is idempotent and must run on
// every exit path, including cancellation and client disconnects.
type Lease struct {
	manager   *Manager
	accountID int
	once      sync.Once
}

func (l *Lease) Release() {
	l.once.Do(func() {
		l.manager.release(l.accountID)
	})
}

func (l *Lease) AccountID() int {
	return l.accountID
}

// Default is the process-wide lease manager. The global cap keeps a
// subscription outage from consuming the process budget that API traffic
// needs; 0 disables the cap.
var Default = NewManager(config.UpstreamMaxInflight)
