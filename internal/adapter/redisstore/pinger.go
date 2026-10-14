package redisstore

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Pinger checks that Redis answers, for the readiness probe.
type Pinger struct {
	client redis.Cmdable
}

// NewPinger returns a Pinger that checks the server of client.
func NewPinger(client redis.Cmdable) *Pinger {
	return &Pinger{client: client}
}

// Ping sends PING to Redis.
func (p *Pinger) Ping(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}
