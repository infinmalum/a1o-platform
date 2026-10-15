package background

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestStoreEvictionCancelsTheOldestRunner pins that capacity eviction
// cancels the in-flight task it drops: the entry leaves the map, but its
// runner must observe the cancellation instead of running on unreferenced.
func TestStoreEvictionCancelsTheOldestRunner(t *testing.T) {
	store := NewStore(time.Hour)
	// Buffered: the runner may observe the cancellation before the test
	// reaches its receive.
	firstDone := make(chan struct{}, 1)
	var firstID string
	for i := 0; i < maxEntries; i++ {
		entry := store.Start(context.Background(), 1, "m", func(ctx context.Context, w http.ResponseWriter) {
			<-ctx.Done()
			if ctx.Err() != nil {
				firstDone <- struct{}{}
			}
		})
		if i == 0 {
			firstID = entry.ID()
		}
	}
	store.Start(context.Background(), 1, "m", func(ctx context.Context, w http.ResponseWriter) {
		<-ctx.Done()
	})

	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("evicted entry's runner was never cancelled")
	}
	if store.Lookup(firstID) != nil {
		t.Fatal("evicted entry is still retrievable")
	}
}
