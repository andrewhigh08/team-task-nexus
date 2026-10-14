package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter limits the requests of each user per minute.
type RateLimiter struct {
	client            *redis.Client
	requestsPerMinute int
}

// NewRateLimiter returns a RateLimiter admitting requestsPerMinute requests per user.
func NewRateLimiter(client *redis.Client, requestsPerMinute int) *RateLimiter {
	return &RateLimiter{
		client:            client,
		requestsPerMinute: requestsPerMinute,
	}
}

// Allow records a request of the user and reports whether it is within the limit.
func (r *RateLimiter) Allow(ctx context.Context, userID int64) (bool, error) {
	key := fmt.Sprintf("rate_limit:user:%d", userID)
	now := time.Now().Unix()
	windowStart := now - 60

	pipe := r.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
	pipe.Expire(ctx, key, 2*time.Minute)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	count := countCmd.Val()
	return count < int64(r.requestsPerMinute), nil
}
