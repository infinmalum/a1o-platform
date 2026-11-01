package agent

import (
	"sync"
)

// Run event bus: in-process fan-out of a running work run's event envelopes
// to live SSE observers. Persisted events also reach observers through the
// events endpoint's replay/poll path (which works across instances); the bus
// exists for transient deltas, which are never persisted. Same-instance
// observers therefore stream text live; observers on other instances see the
// structural events via polling and the final transcript via the run
// projection.

const runSubscriberBuffer = 256

type runSubscriberSet struct {
	mu     sync.Mutex
	nextID int64
	subs   map[int64]chan EventEnvelope
}

var runBus sync.Map // runId int -> *runSubscriberSet

func subscriberSet(runId int) *runSubscriberSet {
	raw, _ := runBus.LoadOrStore(runId, &runSubscriberSet{subs: map[int64]chan EventEnvelope{}})
	return raw.(*runSubscriberSet)
}

// SubscribeRun registers a live observer for one run's envelopes. The
// returned cancel func must be called when the observer leaves.
func SubscribeRun(runId int) (<-chan EventEnvelope, func()) {
	set := subscriberSet(runId)
	set.mu.Lock()
	set.nextID++
	id := set.nextID
	ch := make(chan EventEnvelope, runSubscriberBuffer)
	set.subs[id] = ch
	set.mu.Unlock()
	return ch, func() {
		set.mu.Lock()
		if _, ok := set.subs[id]; ok {
			delete(set.subs, id)
			close(ch)
		}
		set.mu.Unlock()
	}
}

// PublishRun fans one envelope out to the run's observers. Delivery is
// best-effort: a slow or stalled observer misses the envelope rather than
// blocking the worker — persisted state always remains the truth.
func PublishRun(runId int, event EventEnvelope) {
	set := subscriberSet(runId)
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, ch := range set.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
