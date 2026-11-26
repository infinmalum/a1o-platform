package monitor

import "github.com/infinmalum/a1o-platform/common/config"

type channelMetric struct {
	channelID int
	success   bool
}

var metrics = make(chan channelMetric, config.MetricSuccessChanSize+config.MetricFailChanSize)

func consumeMetrics(events <-chan channelMetric) {
	history := make(map[int][]bool)
	for metric := range events {
		window := history[metric.channelID]
		if len(window) == config.MetricQueueSize {
			window = window[1:]
		}
		window = append(window, metric.success)
		history[metric.channelID] = window
		if metric.success || len(window) < config.MetricQueueSize {
			continue
		}
		successes := 0
		for _, success := range window {
			if success {
				successes++
			}
		}
		rate := float64(successes) / float64(len(window))
		if rate < config.MetricSuccessRateThreshold {
			delete(history, metric.channelID)
			go MetricDisableChannel(metric.channelID, rate)
		}
	}
}

func init() {
	if config.EnableMetric && config.MetricQueueSize > 0 {
		go consumeMetrics(metrics)
	}
}

func Emit(channelId int, success bool) {
	if config.EnableMetric && config.MetricQueueSize > 0 {
		metrics <- channelMetric{channelID: channelId, success: success}
	}
}
