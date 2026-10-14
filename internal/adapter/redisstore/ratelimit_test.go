package redisstore_test

import (
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/redisstore"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

const testKey = "rl:test"

var testStart = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// newRateLimiter returns a limiter on a fresh miniredis whose clock stands at testStart. The
// clock drives both TIME, which the script reads, and key expiry.
func newRateLimiter(t *testing.T) (*redisstore.RateLimiter, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(testStart)
	mr.ClockTTL(true)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return redisstore.NewRateLimiter(client), mr, client
}

func allowed(remaining int) port.RateDecision {
	return port.RateDecision{Allowed: true, Remaining: remaining}
}

func rejected(retryAfter time.Duration) port.RateDecision {
	return port.RateDecision{RetryAfter: retryAfter}
}

func TestRateLimiter_Allow(t *testing.T) {
	t.Parallel()
	type request struct {
		at    time.Duration // offset from testStart
		limit int
		want  port.RateDecision
	}
	tests := []struct {
		name     string
		requests []request
	}{
		{
			name: "admits up to the limit, then rejects until the oldest request leaves the window",
			requests: []request{
				{at: 0, limit: 3, want: allowed(2)},
				{at: 10 * time.Second, limit: 3, want: allowed(1)},
				{at: 20 * time.Second, limit: 3, want: allowed(0)},
				{at: 30 * time.Second, limit: 3, want: rejected(30 * time.Second)},
			},
		},
		{
			name: "window slides",
			requests: []request{
				{at: 0, limit: 2, want: allowed(1)},
				{at: 20 * time.Second, limit: 2, want: allowed(0)},
				{at: 50 * time.Second, limit: 2, want: rejected(10 * time.Second)},
				{at: time.Minute, limit: 2, want: allowed(0)},
				{at: 61 * time.Second, limit: 2, want: rejected(19 * time.Second)},
			},
		},
		{
			name: "rejected requests do not extend the window",
			requests: []request{
				{at: 0, limit: 1, want: allowed(0)},
				{at: 30 * time.Second, limit: 1, want: rejected(30 * time.Second)},
				{at: 59 * time.Second, limit: 1, want: rejected(time.Second)},
				{at: time.Minute, limit: 1, want: allowed(0)},
			},
		},
		{
			name: "retry after is rounded up to whole milliseconds",
			requests: []request{
				{at: 0, limit: 1, want: allowed(0)},
				{at: 1500 * time.Microsecond, limit: 1, want: rejected(59999 * time.Millisecond)},
			},
		},
		{
			name: "a lowered limit waits until enough requests have left the window",
			requests: []request{
				{at: 0, limit: 3, want: allowed(2)},
				{at: 10 * time.Second, limit: 3, want: allowed(1)},
				{at: 20 * time.Second, limit: 3, want: allowed(0)},
				{at: 30 * time.Second, limit: 1, want: rejected(50 * time.Second)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter, mr, _ := newRateLimiter(t)

			for i, req := range tt.requests {
				mr.SetTime(testStart.Add(req.at))

				got, err := limiter.Allow(t.Context(), testKey, req.limit, time.Minute)

				require.NoError(t, err, "request %d", i+1)
				assert.Equal(t, req.want, got, "request %d at +%s", i+1, req.at)
			}
		})
	}
}

func TestRateLimiter_Allow_RecordsAdmittedRequestsWithTheRedisClock(t *testing.T) {
	t.Parallel()
	limiter, mr, _ := newRateLimiter(t)

	for range 2 {
		_, err := limiter.Allow(t.Context(), testKey, 2, time.Minute)
		require.NoError(t, err)
	}
	_, err := limiter.Allow(t.Context(), testKey, 2, time.Minute)
	require.NoError(t, err)

	members, err := mr.ZMembers(testKey)
	require.NoError(t, err)
	require.Len(t, members, 2, "the rejected request is not recorded")
	micros := testStart.UnixMicro()
	member := regexp.MustCompile(`^` + strconv.FormatInt(micros, 10) + `-[0-9a-f]{16}$`)
	for _, m := range members {
		assert.Regexp(t, member, m, "members are <microseconds>-<random hex>")
		score, err := mr.ZScore(testKey, m)
		require.NoError(t, err)
		assert.InDelta(t, float64(micros), score, 0, "scored by the Redis clock in microseconds")
	}
}

func TestRateLimiter_Allow_KeyExpiresWithTheWindow(t *testing.T) {
	t.Parallel()
	limiter, mr, _ := newRateLimiter(t)

	_, err := limiter.Allow(t.Context(), testKey, 5, 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, mr.TTL(testKey))

	mr.SetTime(testStart.Add(4 * time.Second))
	_, err = limiter.Allow(t.Context(), testKey, 5, 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, mr.TTL(testKey), "every admitted request renews the expiry")

	mr.SetTime(testStart.Add(14 * time.Second))
	assert.False(t, mr.Exists(testKey), "an idle key disappears once its window has passed")
}

func TestRateLimiter_Allow_KeysAreIndependent(t *testing.T) {
	t.Parallel()
	limiter, _, _ := newRateLimiter(t)

	first, err := limiter.Allow(t.Context(), "rl:api:user:1", 1, time.Minute)
	require.NoError(t, err)
	limited, err := limiter.Allow(t.Context(), "rl:api:user:1", 1, time.Minute)
	require.NoError(t, err)
	other, err := limiter.Allow(t.Context(), "rl:api:user:2", 1, time.Minute)
	require.NoError(t, err)

	assert.True(t, first.Allowed)
	assert.False(t, limited.Allowed)
	assert.True(t, other.Allowed, "another key has a window of its own")
}

func TestRateLimiter_Allow_ConcurrentRequestsNeverExceedTheLimit(t *testing.T) {
	t.Parallel()
	const (
		goroutines   = 20
		perGoroutine = 10
		limit        = 75
	)
	limiter, mr, _ := newRateLimiter(t)

	var admitted, rejectedCount, failed atomic.Int64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perGoroutine {
				decision, err := limiter.Allow(t.Context(), testKey, limit, time.Minute)
				switch {
				case err != nil:
					failed.Add(1)
				case decision.Allowed:
					admitted.Add(1)
				default:
					rejectedCount.Add(1)
				}
			}
		})
	}
	wg.Wait()

	require.Zero(t, failed.Load(), "failed requests")
	assert.Equal(t, int64(limit), admitted.Load())
	assert.Equal(t, int64(goroutines*perGoroutine-limit), rejectedCount.Load())
	members, err := mr.ZMembers(testKey)
	require.NoError(t, err)
	assert.Len(t, members, limit, "requests of the same microsecond are recorded apart")
}

func TestRateLimiter_Allow_ReloadsAFlushedScript(t *testing.T) {
	t.Parallel()
	limiter, _, client := newRateLimiter(t)
	_, err := limiter.Allow(t.Context(), testKey, 2, time.Minute)
	require.NoError(t, err)
	require.NoError(t, client.ScriptFlush(t.Context()).Err())

	got, err := limiter.Allow(t.Context(), testKey, 2, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, allowed(0), got)
}

func TestRateLimiter_Allow_RejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		limit  int
		window time.Duration
	}{
		{name: "zero limit", limit: 0, window: time.Minute},
		{name: "negative limit", limit: -1, window: time.Minute},
		{name: "zero window", limit: 1, window: 0},
		{name: "window below a millisecond", limit: 1, window: time.Millisecond - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter, mr, _ := newRateLimiter(t)

			_, err := limiter.Allow(t.Context(), testKey, tt.limit, tt.window)

			require.ErrorContains(t, err, "limit must be positive and window at least 1ms")
			assert.Zero(t, mr.CommandCount(), "nothing reaches Redis")
		})
	}
}

func TestRateLimiter_Allow_FailsWhenRedisIsDown(t *testing.T) {
	t.Parallel()
	limiter, mr, _ := newRateLimiter(t)
	mr.Close()

	got, err := limiter.Allow(t.Context(), testKey, 1, time.Minute)

	require.Error(t, err)
	assert.Contains(t, err.Error(), testKey)
	assert.Equal(t, port.RateDecision{}, got)
}
