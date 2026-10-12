package account

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLeaseEnforcesAccountLimit(t *testing.T) {
	manager := NewManager(0)
	lease, err := manager.Acquire(context.Background(), 1, 1, 0)
	require.NoError(t, err)
	require.Equal(t, 1, manager.Held(1))

	// maxWait 0 fails immediately when the account is full.
	_, err = manager.Acquire(context.Background(), 1, 1, 0)
	require.ErrorIs(t, err, ErrCapacity)

	// A different account is unaffected.
	other, err := manager.Acquire(context.Background(), 2, 1, 0)
	require.NoError(t, err)

	lease.Release()
	other.Release()
	require.Equal(t, 0, manager.Held(1))
	require.Equal(t, 0, manager.Held(2))
}

func TestLeaseReleaseWakesWaiter(t *testing.T) {
	manager := NewManager(0)
	first, err := manager.Acquire(context.Background(), 1, 1, 0)
	require.NoError(t, err)

	acquired := make(chan *Lease, 1)
	go func() {
		lease, err := manager.Acquire(context.Background(), 1, 1, 5*time.Second)
		require.NoError(t, err)
		acquired <- lease
	}()
	// Give the waiter time to register before releasing.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, 1, manager.Held(1))
	first.Release()

	select {
	case lease := <-acquired:
		require.Equal(t, 1, manager.Held(1))
		lease.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was not woken by release")
	}
}

func TestLeaseBoundedWaitTimesOut(t *testing.T) {
	manager := NewManager(0)
	lease, err := manager.Acquire(context.Background(), 1, 1, 0)
	require.NoError(t, err)

	start := time.Now()
	_, err = manager.Acquire(context.Background(), 1, 1, 80*time.Millisecond)
	require.ErrorIs(t, err, ErrCapacity)
	require.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond)
	lease.Release()
}

func TestLeaseRespectsContextCancellation(t *testing.T) {
	manager := NewManager(0)
	lease, err := manager.Acquire(context.Background(), 1, 1, 0)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.Acquire(ctx, 1, 1, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
	lease.Release()
}

func TestLeaseGlobalCap(t *testing.T) {
	manager := NewManager(2)
	first, err := manager.Acquire(context.Background(), 1, 0, 0)
	require.NoError(t, err)
	second, err := manager.Acquire(context.Background(), 2, 0, 0)
	require.NoError(t, err)

	// The per-account limits allow more, but the global budget is exhausted.
	_, err = manager.Acquire(context.Background(), 3, 0, 0)
	require.ErrorIs(t, err, ErrCapacity)

	first.Release()
	third, err := manager.Acquire(context.Background(), 3, 0, 0)
	require.NoError(t, err)
	second.Release()
	third.Release()
}

func TestLeaseReleaseIsIdempotent(t *testing.T) {
	manager := NewManager(0)
	lease, err := manager.Acquire(context.Background(), 1, 1, 0)
	require.NoError(t, err)
	lease.Release()
	lease.Release()
	require.Equal(t, 0, manager.Held(1))
	// The freed slot is usable exactly once.
	next, err := manager.Acquire(context.Background(), 1, 1, 0)
	require.NoError(t, err)
	next.Release()
}

func TestLeaseUnlimitedWhenLimitZero(t *testing.T) {
	manager := NewManager(0)
	var leases []*Lease
	for i := 0; i < 5; i++ {
		lease, err := manager.Acquire(context.Background(), 7, 0, 0)
		require.NoError(t, err)
		leases = append(leases, lease)
	}
	require.Equal(t, 5, manager.Held(7))
	for _, lease := range leases {
		lease.Release()
	}
	require.Equal(t, 0, manager.Held(7))
}

func TestLeaseConcurrentAcquireRelease(t *testing.T) {
	manager := NewManager(0)
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				lease, err := manager.Acquire(context.Background(), 1, 4, 2*time.Second)
				require.NoError(t, err)
				require.LessOrEqual(t, manager.Held(1), 4)
				lease.Release()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 0, manager.Held(1))
}
