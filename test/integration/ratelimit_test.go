package integration

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/config"
)

// TestRateLimits boots an application of its own with small limits, sharing the databases of
// the suite; the shared application is never throttled. The limited one trusts proxy headers,
// so the test can pose as clients of its choosing.
func TestRateLimits(t *testing.T) {
	t.Parallel()
	cfg := *env(t).cfg
	cfg.RateLimit = config.RateLimitConfig{AuthPerMinute: 3, APIPerMinute: 5}
	cfg.Server.TrustProxyHeaders = true
	_, limited := bootApp(t, &cfg)
	acc := newClient(t).mustRegister(t, "Throttled")

	t.Run("logins per client address", func(t *testing.T) {
		// A /64 of its own, so that earlier runs against the same Redis do not count.
		network := randomIPv6Network()
		login := func(addr string) apiResponse {
			req := limited.newRequest(t, http.MethodPost, "/api/v1/login", map[string]string{"email": acc.Email, "password": password}, "")
			req.Header.Set("X-Forwarded-For", addr)
			return limited.send(t, req)
		}
		for i := range 3 {
			resp := login(network + "::1")
			requireStatus(t, resp, http.StatusOK)
			assert.Equal(t, "3", resp.Header.Get("X-RateLimit-Limit"))
			assert.Equal(t, strconv.Itoa(2-i), resp.Header.Get("X-RateLimit-Remaining"))
		}

		// IPv6 clients are limited by their /64, so another address in it does not help.
		rejected := login(network + "::2")
		errBody := requireError(t, rejected, http.StatusTooManyRequests, "TOO_MANY_REQUESTS")
		assert.Equal(t, "0", rejected.Header.Get("X-RateLimit-Remaining"))
		retryAfter, err := strconv.Atoi(rejected.Header.Get("Retry-After"))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, retryAfter, 1)
		assert.LessOrEqual(t, retryAfter, 60)
		assert.EqualValues(t, retryAfter, errBody.Details["retry_after_seconds"])

		requireStatus(t, login(randomIPv6Network()+"::1"), http.StatusOK)
	})

	t.Run("requests per user", func(t *testing.T) {
		for i := range 5 {
			resp := limited.do(t, http.MethodGet, "/api/v1/teams", nil, acc.Token)
			requireStatus(t, resp, http.StatusOK)
			assert.Equal(t, strconv.Itoa(4-i), resp.Header.Get("X-RateLimit-Remaining"))
		}
		rejected := limited.do(t, http.MethodGet, "/api/v1/teams", nil, acc.Token)
		requireError(t, rejected, http.StatusTooManyRequests, "TOO_MANY_REQUESTS")
		assert.Equal(t, "5", rejected.Header.Get("X-RateLimit-Limit"))
		assert.NotEmpty(t, rejected.Header.Get("Retry-After"))

		other := newClient(t).mustRegister(t, "Unthrottled")
		requireStatus(t, limited.do(t, http.MethodGet, "/api/v1/teams", nil, other.Token), http.StatusOK)
	})
}

// randomIPv6Network returns the first four groups of a random /64 in the documentation range
// 2001:db8::/32.
func randomIPv6Network() string {
	return fmt.Sprintf("2001:db8:%x:%x", rand.IntN(1<<16), rand.IntN(1<<16))
}
