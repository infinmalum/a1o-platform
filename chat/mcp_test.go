package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/infinmalum/a1o-platform/model"
)

// brokenMCPServer serves an HTTP endpoint that fails the MCP handshake (the
// SDK retries the initialize once, so one dial means a couple of requests).
// The first request sleeps for delay when it is positive.
func brokenMCPServer(t *testing.T, delay time.Duration) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 && delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func mcpServerRow(id int, name, url string, updatedTime int64) *model.ChatMCPServer {
	return &model.ChatMCPServer{
		Id: id, Name: name, Transport: model.ChatMCPServerHTTP,
		URL: url, Enabled: true, UpdatedTime: updatedTime,
	}
}

// TestMCPFailureNegativeCache pins the backoff: a server that just failed is
// not retried on the next turn, so a dead endpoint stalls one turn instead of
// every turn.
func TestMCPFailureNegativeCache(t *testing.T) {
	server, hits := brokenMCPServer(t, 0)
	manager := NewMCPManager()
	row := mcpServerRow(1, "broken", server.URL, 42)

	_, err := manager.ensure(context.Background(), row)
	require.Error(t, err)
	afterFirstDial := atomic.LoadInt32(hits)

	_, err = manager.ensure(context.Background(), row)
	require.Error(t, err)

	require.Equal(t, afterFirstDial, atomic.LoadInt32(hits), "second ensure must be served from the failure cache")
}

// TestMCPConcurrentConnectsCollapseIntoOne pins the per-server singleflight:
// concurrent turns needing the same server dial it once. The request count of
// one dial is calibrated on an identical server first, because the SDK's
// handshake retry behavior may change between versions.
func TestMCPConcurrentConnectsCollapseIntoOne(t *testing.T) {
	calibrationServer, calibrationHits := brokenMCPServer(t, 0)
	_, err := NewMCPManager().ensure(context.Background(), mcpServerRow(1, "calibration", calibrationServer.URL, 42))
	require.Error(t, err)
	requestsPerDial := atomic.LoadInt32(calibrationHits)
	require.Positive(t, requestsPerDial)

	server, hits := brokenMCPServer(t, 200*time.Millisecond)
	manager := NewMCPManager()
	row := mcpServerRow(1, "broken", server.URL, 42)

	const callers = 5
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.ensure(context.Background(), row)
			require.Error(t, err)
		}()
	}
	wg.Wait()

	require.Equal(t, requestsPerDial, atomic.LoadInt32(hits), "concurrent ensures must dial once")
}

// TestMCPSlowServersDoNotSerialize guards the lock scope: two different slow
// servers connect in parallel, so one bad server no longer queues every
// turn behind its own dial.
func TestMCPSlowServersDoNotSerialize(t *testing.T) {
	const dialDelay = 1200 * time.Millisecond
	slowA, _ := brokenMCPServer(t, dialDelay)
	slowB, _ := brokenMCPServer(t, dialDelay)
	manager := NewMCPManager()
	rowA := mcpServerRow(1, "slow-a", slowA.URL, 1)
	rowB := mcpServerRow(2, "slow-b", slowB.URL, 1)

	start := time.Now()
	var wg sync.WaitGroup
	for _, row := range []*model.ChatMCPServer{rowA, rowB} {
		wg.Add(1)
		go func(row *model.ChatMCPServer) {
			defer wg.Done()
			_, err := manager.ensure(context.Background(), row)
			require.Error(t, err)
		}(row)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Serialized dials (the old global-lock behavior) would take at least
	// twice the slow dial; parallel dials finish in about one.
	require.Less(t, elapsed, 2*dialDelay, "different servers must connect in parallel, took %v", elapsed)
}

// TestMCPFailureBackoffExpiresAfterEdit verifies an edited server row (new
// UpdatedTime) is retried immediately despite a cached failure for the old
// version.
func TestMCPFailureBackoffExpiresAfterEdit(t *testing.T) {
	server, hits := brokenMCPServer(t, 0)
	manager := NewMCPManager()

	_, err := manager.ensure(context.Background(), mcpServerRow(1, "broken", server.URL, 42))
	require.Error(t, err)
	afterFirstDial := atomic.LoadInt32(hits)

	_, err = manager.ensure(context.Background(), mcpServerRow(1, "broken", server.URL, 43))
	require.Error(t, err)

	require.Greater(t, atomic.LoadInt32(hits), afterFirstDial, "an edited server must be retried despite the old version's failure cache")
}
