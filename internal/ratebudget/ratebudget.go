// Package ratebudget spends a workspace's Slack API rate limits (spec
// sync-engine, "Rate-limit budget"): a budget per API method, interactive
// work ahead of background work, and Retry-After honored.
package ratebudget

import (
	"context"
	"time"
)

// Priority orders calls competing for the same method's budget.
type Priority uint8

const (
	// Background is sync work nobody is waiting for: backfill, presence.
	Background Priority = iota
	// Interactive is a user waiting: opening a conversation, sending.
	Interactive
)

// Limit is the budget of one Slack API method.
type Limit struct {
	PerMinute int // sustained calls per minute
	Burst     int // calls allowed at once after a quiet period
}

// Budget hands out permission to call Slack API methods. Implementations
// must be safe for concurrent use and must take their clock as a
// dependency, so tests never sleep.
type Budget interface {
	// Wait blocks until method may be called at priority p, or ctx ends.
	// While both are waiting on the same method, Interactive callers go
	// before Background ones.
	Wait(ctx context.Context, method string, p Priority) error

	// Penalize records an HTTP 429: method gets no calls until
	// retryAfter has passed.
	Penalize(method string, retryAfter time.Duration)
}
