package middleware

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

const (
	// rateLimitWindow is the period the configured per-minute limits are counted over.
	rateLimitWindow = time.Minute

	userKeyPrefix = "rl:api:user:"
	ipKeyPrefix   = "rl:auth:ip:"

	// ipv6ClientBits is the prefix length that identifies one IPv6 client: a subscriber is
	// usually assigned at least a /64, so keying single addresses would let it rotate freely.
	ipv6ClientBits = 64
	// unknownClient keys the requests whose address cannot be parsed. They share one window,
	// so a broken address is limited rather than exempt.
	unknownClient = "unknown"

	headerRateLimitLimit     = "X-RateLimit-Limit"
	headerRateLimitRemaining = "X-RateLimit-Remaining"
	headerRetryAfter         = "Retry-After"
)

// RateLimitByUser limits every authenticated user to perMinute requests per minute across all
// replicas. It must run after Authenticate: a request without a user is answered with 401.
func RateLimitByUser(limiter port.RateLimiter, perMinute int) func(http.Handler) http.Handler {
	rl := rateLimit{limiter: limiter, scope: metrics.ScopeAPI, perMinute: perMinute}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFrom(r.Context())
			if !ok {
				unauthorized(w, r, "")
				return
			}
			rl.serve(w, r, next, userKeyPrefix+strconv.FormatInt(userID, 10))
		})
	}
}

// RateLimitByIP limits every client address to perMinute requests per minute across all
// replicas. It guards registration and login, which have no user to key on yet. The address
// comes from ClientIP with trustProxy; IPv6 clients are keyed by their /64 prefix.
func RateLimitByIP(limiter port.RateLimiter, perMinute int, trustProxy bool) func(http.Handler) http.Handler {
	rl := rateLimit{limiter: limiter, scope: metrics.ScopeAuth, perMinute: perMinute}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rl.serve(w, r, next, ipKeyPrefix+clientKey(ClientIP(r, trustProxy)))
		})
	}
}

// rateLimit applies one limit, counted under scope in the metrics.
type rateLimit struct {
	limiter   port.RateLimiter
	scope     string
	perMinute int
}

// serve passes the request to next when the limiter admits it under key and answers 429
// otherwise. When the limiter fails, the request is let through: the limits protect capacity,
// not secrets, and an outage of Redis must not take the whole API down with it.
func (rl rateLimit) serve(w http.ResponseWriter, r *http.Request, next http.Handler, key string) {
	w.Header().Set(headerRateLimitLimit, strconv.Itoa(rl.perMinute))
	decision, err := rl.limiter.Allow(r.Context(), key, rl.perMinute, rateLimitWindow)
	if err != nil {
		metrics.RecordRateLimitDecision(rl.scope, metrics.ResultError)
		logger.FromContext(r.Context()).Warn("rate limiter failed, request let through", "scope", rl.scope, "error", err)
		next.ServeHTTP(w, r)
		return
	}

	w.Header().Set(headerRateLimitRemaining, strconv.Itoa(decision.Remaining))
	if !decision.Allowed {
		metrics.RecordRateLimitDecision(rl.scope, metrics.ResultLimited)
		retryAfter := retryAfterSeconds(decision.RetryAfter)
		w.Header().Set(headerRetryAfter, strconv.Itoa(retryAfter))
		response.Error(w, r, apperror.TooManyRequests("rate limit exceeded", retryAfter))
		return
	}
	metrics.RecordRateLimitDecision(rl.scope, metrics.ResultAllowed)
	next.ServeHTTP(w, r)
}

// clientKey returns the part of a rate limit key that identifies the client at addr: the
// address itself for IPv4, the /64 prefix for IPv6.
func clientKey(addr netip.Addr) string {
	if !addr.IsValid() {
		return unknownClient
	}
	if addr.Is4() {
		return addr.String()
	}
	// Cannot fail: a valid IPv6 address has more than ipv6ClientBits bits.
	prefix, _ := addr.Prefix(ipv6ClientBits)
	return prefix.String()
}

// retryAfterSeconds rounds wait up to the whole seconds of Retry-After and never answers
// less than one: "0" would invite a retry that is still too early.
func retryAfterSeconds(wait time.Duration) int {
	return max(1, int(math.Ceil(wait.Seconds())))
}
