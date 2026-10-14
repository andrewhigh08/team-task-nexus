package redisstore

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// memberTokenBytes is the size of the random part of a window entry: 16 hex characters.
const memberTokenBytes = 8

//go:embed ratelimit.lua
var slidingWindowSource string

// slidingWindow runs by EVALSHA and falls back to EVAL when the server does not have the
// script cached yet, for example after a restart.
var slidingWindow = redis.NewScript(slidingWindowSource)

// RateLimiter is a sliding-window log limiter. Every decision is a single Lua script, so
// concurrent requests, also from other replicas, can never be admitted beyond the limit.
type RateLimiter struct {
	client redis.Scripter
}

// NewRateLimiter returns a RateLimiter keeping its windows on client.
func NewRateLimiter(client redis.Scripter) *RateLimiter {
	return &RateLimiter{client: client}
}

// Allow reports whether one more request under key fits into limit requests per window and
// records it when it does. The window is kept to whole milliseconds.
func (l *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (port.RateDecision, error) {
	if limit < 1 || window < time.Millisecond {
		return port.RateDecision{}, fmt.Errorf("rate limit %d per %s: limit must be positive and window at least 1ms", limit, window)
	}
	reply, err := slidingWindow.Run(ctx, l.client, []string{key}, limit, window.Milliseconds(), memberToken()).Int64Slice()
	if err != nil {
		return port.RateDecision{}, fmt.Errorf("rate limit %s: %w", key, err)
	}
	if len(reply) != 3 {
		return port.RateDecision{}, fmt.Errorf("rate limit %s: unexpected reply %v", key, reply)
	}
	return port.RateDecision{
		Allowed:    reply[0] == 1,
		Remaining:  int(reply[1]),
		RetryAfter: time.Duration(reply[2]) * time.Millisecond,
	}, nil
}

// memberToken returns the random part of a window entry. The time alone is not unique: two
// requests can arrive within the same microsecond.
func memberToken() string {
	buf := make([]byte, memberTokenBytes)
	// crypto/rand.Read never returns an error; it crashes the program instead.
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
