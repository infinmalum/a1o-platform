package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The events stream must answer an unusable cursor with an explicit resync
// (runtime plan §4): retained history pruned below the cursor, a cursor at
// or beyond the log head, and an entirely pruned log all desynchronize the
// client; a cursor inside the intact log replays normally.
func TestEventResyncTarget(t *testing.T) {
	// Fresh observation (after=0) never resyncs — replay from the start.
	target, resync, reset := eventResyncTarget(0, 1, 10, true)
	require.False(t, resync)
	require.False(t, reset)
	require.Equal(t, int64(0), target)

	// Cursor inside the intact log: normal replay.
	target, resync, reset = eventResyncTarget(4, 1, 10, true)
	require.False(t, resync)

	// Gap below the cursor — history pruned by retention: resync to the head,
	// keep the client's timeline (it was built from events it actually saw).
	target, resync, reset = eventResyncTarget(4, 8, 10, true)
	require.True(t, resync)
	require.False(t, reset)
	require.Equal(t, int64(10), target)

	// Cursor at or beyond the log head — a resumed run whose pruned log
	// restarted sequences: resync to the head and rebuild the timeline.
	target, resync, reset = eventResyncTarget(50, 1, 10, true)
	require.True(t, resync)
	require.True(t, reset)
	require.Equal(t, int64(10), target)

	// Entirely pruned log: resync to zero and rebuild.
	target, resync, reset = eventResyncTarget(7, 0, 0, false)
	require.True(t, resync)
	require.True(t, reset)
	require.Equal(t, int64(0), target)
}
