package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	client           *redis.Client
	requestsPerMinute int
}

func NewRateLimiter(client *redis.Client, requestsPerMinute int) *RateLimiter {
	return &RateLimiter{
		client:           client,
		requestsPerMinute: requestsPerMinute,
	}
}

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
