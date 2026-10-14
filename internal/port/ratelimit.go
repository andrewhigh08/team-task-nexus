package port

import (
	"context"
	"time"
)

// RateDecision is a RateLimiter's answer to one request.
type RateDecision struct {
	Allowed bool
	// Remaining is the number of further requests the current window admits.
	Remaining int
	// RetryAfter is how long a rejected client has to wait until a request is admitted again;
	// zero for an allowed request.
	RetryAfter time.Duration
}

// RateLimiter counts requests per key over a sliding window shared by every replica of the
// service.
type RateLimiter interface {
	// Allow reports whether one more request under key fits into limit requests per window and
	// records it when it does. Rejected requests are not recorded, so a client retrying too
	// early does not push its own window further out.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (RateDecision, error)
}
