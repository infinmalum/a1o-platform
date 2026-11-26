package monitor

import (
	"sync"
	"testing"
)

func TestConcurrentMetricProducers(t *testing.T) {
	events := make(chan channelMetric, 32)
	done := make(chan struct{})
	go func() {
		consumeMetrics(events)
		close(done)
	}()
	var producers sync.WaitGroup
	for _, success := range []bool{true, false} {
		producers.Go(func() {
			for id := 0; id < 10000; id++ {
				events <- channelMetric{channelID: id, success: success}
			}
		})
	}
	producers.Wait()
	close(events)
	<-done
}
