package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/native"
)

type NativeReservation struct {
	UserID            int
	TokenID           int
	ChannelID         int
	ChannelType       int
	TokenName         string
	ModelName         string
	Group             string
	Reserved          int64
	ModelRatio        float64
	GroupRatio        float64
	CompletionRatio   float64
	CacheReadRatio    float64
	CacheWriteRatio   float64
	CacheWrite1hRatio float64
	SystemPromptReset bool
	StartedAt         time.Time
	quota             *Reservation
	mu                sync.Mutex
	finished          bool
	settlement        *Settlement
}

// Settlement carries the observed provider counters and the actual charge.
type Settlement struct {
	native.Usage
	ConsumeLogID int    `json:"-"`
	Quota        int64  `json:"quota"`
	UsageState   string `json:"usage_state"`
	ChargeBasis  string `json:"charge_basis"`
	Interrupted  bool   `json:"interrupted"`
}

// ReserveNativeQuota uses the provider's output limit plus the configured
// input allowance as the provisional charge. The final provider usage replaces
// this charge after the response, including when a stream is interrupted.
// The reservation is taken by pointer: the struct carries a mutex, so it must
// never be copied.
func ReserveNativeQuota(ctx context.Context, reservation *NativeReservation, maxOutputTokens int64) (*NativeReservation, error) {
	if reservation == nil {
		return nil, errors.New("reservation is nil")
	}
	if maxOutputTokens < 0 {
		return nil, errors.New("output token limit must not be negative")
	}
	modelRatio, err := ratio.GetModelRatio(reservation.ModelName)
	if err != nil {
		return nil, err
	}
	completionRatio, err := ratio.GetCompletionRatio(reservation.ModelName)
	if err != nil {
		return nil, err
	}
	reservation.ModelRatio = modelRatio
	reservation.GroupRatio = ratio.GetGroupRatio(reservation.Group)
	reservation.CompletionRatio = completionRatio
	cache := ratio.LookupCachePrices(reservation.ModelName)
	reservation.CacheReadRatio, reservation.CacheWriteRatio = modelRatio, modelRatio
	if cache.Read != nil {
		reservation.CacheReadRatio = *cache.Read
	}
	if cache.Write != nil {
		reservation.CacheWriteRatio = *cache.Write
	}
	reservation.CacheWrite1hRatio = reservation.CacheWriteRatio
	if cache.Write1h != nil {
		reservation.CacheWrite1hRatio = *cache.Write1h
	}
	reservation.StartedAt = time.Now()
	reservation.Reserved = int64(math.Ceil(float64(config.PreConsumedQuota+maxOutputTokens) * reservation.ModelRatio * reservation.GroupRatio))
	if reservation.Reserved < 0 {
		return nil, errors.New("invalid quota reservation")
	}
	reservation.quota, err = Reserve(ctx, reservation.UserID, reservation.TokenID, reservation.Reserved)
	if err != nil {
		return nil, err
	}
	return reservation, nil
}

// ReserveFixedQuota reserves an exact operation charge, such as image size
// pricing or audio input length, instead of a token-based estimate. The
// amounts themselves come from the caller's ratio computation.
func ReserveFixedQuota(ctx context.Context, reservation *NativeReservation, amount int64) (*NativeReservation, error) {
	if reservation == nil {
		return nil, errors.New("reservation is nil")
	}
	if amount < 0 {
		return nil, errors.New("fixed quota must not be negative")
	}
	modelRatio, err := ratio.GetModelRatio(reservation.ModelName)
	if err != nil {
		return nil, err
	}
	reservation.ModelRatio = modelRatio
	reservation.GroupRatio = ratio.GetGroupRatio(reservation.Group)
	// A fixed charge bills one computed amount and has no output component,
	// so no completion price participates.
	reservation.CompletionRatio = 0
	reservation.StartedAt = time.Now()
	reservation.Reserved = amount
	reservation.quota, err = Reserve(ctx, reservation.UserID, reservation.TokenID, amount)
	if err != nil {
		return nil, err
	}
	return reservation, nil
}

func (r *NativeReservation) Refund(ctx context.Context) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.quota.Refund(ctx)
	r.finished = true
}

// Settle retains at least the reservation for an interrupted stream because a
// partial usage report may omit tokens already generated upstream.
func (r *NativeReservation) Settle(ctx context.Context, usage native.Usage, streamed, interrupted bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	settlement := &Settlement{Usage: usage, UsageState: "missing", ChargeBasis: "reservation", Interrupted: interrupted}
	quota := r.Reserved
	if usage.Seen {
		settlement.UsageState = "reported"
		if streamed && (!usage.Complete || usage.Partial) {
			settlement.UsageState = "partial"
		}
		settlement.ChargeBasis = "usage"
		totalInput := max(0, usage.Input)
		read := min(max(0, usage.CacheRead), totalInput)
		write := min(max(0, usage.CacheCreation), totalInput-read)
		write1h := min(max(0, usage.CacheCreation1h), write)
		input := float64(totalInput-read-write)*r.ModelRatio + float64(read)*r.CacheReadRatio +
			float64(write-write1h)*r.CacheWriteRatio + float64(write1h)*r.CacheWrite1hRatio
		output := float64(usage.Output) * r.CompletionRatio * r.ModelRatio
		quota = int64(math.Ceil((input + output) * r.GroupRatio))
	}
	if (interrupted || settlement.UsageState == "partial") && quota < r.Reserved {
		quota = r.Reserved
		settlement.ChargeBasis = "reservation"
	}
	settlement.Quota = r.quota.Settle(ctx, quota)
	if settlement.Quota != quota {
		settlement.ChargeBasis = "reservation"
	}
	r.settlement = settlement
	r.finished = true
	quota = settlement.Quota
	if quota == 0 {
		return
	}
	content := fmt.Sprintf("native protocol; ratio: %.2f × %.2f × %.2f", r.ModelRatio, r.GroupRatio, r.CompletionRatio)
	if !usage.Seen {
		content += "; upstream usage unavailable, reserved quota retained"
	}
	if interrupted {
		content += "; response interrupted"
	}
	log := &model.Log{
		UserId:             r.UserID,
		ChannelId:          r.ChannelID,
		PromptTokens:       int(usage.Input),
		CacheReadTokens:    usage.CacheRead,
		CacheWriteTokens:   usage.CacheCreation,
		CacheWrite1hTokens: usage.CacheCreation1h,
		CompletionTokens:   int(usage.Output),
		ModelName:          r.ModelName,
		TokenName:          r.TokenName,
		Quota:              int(quota),
		UsageState:         settlement.UsageState,
		ChargeBasis:        settlement.ChargeBasis,
		Interrupted:        interrupted,
		Content:            content,
		IsStream:           streamed,
		ElapsedTime:        time.Since(r.StartedAt).Milliseconds(),
		SystemPromptReset:  r.SystemPromptReset,
	}
	model.RecordConsumeLog(ctx, log)
	// RecordConsumeLog may be disabled or fail; a zero id means the
	// invocation association is unavailable, not that billing was skipped.
	settlement.ConsumeLogID = log.Id
	model.UpdateUserUsedQuotaAndRequestCount(r.UserID, quota)
	model.UpdateChannelUsedQuota(r.ChannelID, quota)
}

func (r *NativeReservation) Settlement() (Settlement, bool) {
	if r == nil {
		return Settlement{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settlement == nil {
		return Settlement{}, false
	}
	return *r.settlement, true
}

// SettleDirect charges an operation-specific amount without token ratio math,
// for operations whose price the gateway already computed (images) or that
// bill on a derived measure of the response (audio transcription text).
func (r *NativeReservation) SettleDirect(ctx context.Context, amount, promptTokens int64, interrupted bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	quota := r.quota.Settle(ctx, amount)
	settlement := &Settlement{Quota: quota, ChargeBasis: "fixed", Interrupted: interrupted}
	r.settlement = settlement
	r.finished = true
	if quota == 0 {
		return
	}
	content := fmt.Sprintf("fixed quota; ratio: %.2f × %.2f", r.ModelRatio, r.GroupRatio)
	if interrupted {
		content += "; response interrupted"
	}
	log := &model.Log{
		UserId:       r.UserID,
		ChannelId:    r.ChannelID,
		PromptTokens: int(promptTokens),
		ModelName:    r.ModelName,
		TokenName:    r.TokenName,
		Quota:        int(quota),
		ChargeBasis:  settlement.ChargeBasis,
		Interrupted:  interrupted,
		Content:      content,
		ElapsedTime:  time.Since(r.StartedAt).Milliseconds(),
	}
	model.RecordConsumeLog(ctx, log)
	settlement.ConsumeLogID = log.Id
	model.UpdateUserUsedQuotaAndRequestCount(r.UserID, quota)
	model.UpdateChannelUsedQuota(r.ChannelID, quota)
}
