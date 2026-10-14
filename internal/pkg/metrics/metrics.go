// Package metrics defines the Prometheus collectors of the service. They are registered with
// the default registry, which GET /metrics exposes, and every name starts with tasknexus_.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Values of the scope label of RateLimitDecisions: the per-user limit of the authenticated
// API and the per-IP limit of registration and login.
const (
	ScopeAPI  = "api"
	ScopeAuth = "auth"
)

// Values of the result label of RateLimitDecisions. ResultError counts requests let through
// because the limiter could not decide.
const (
	ResultAllowed = "allowed"
	ResultLimited = "limited"
	ResultError   = "error"
)

// RateLimitDecisions counts rate limiter decisions by scope and result.
var RateLimitDecisions = newRateLimitDecisions()

// newRateLimitDecisions registers the counter and creates every known series, so each one is
// exported from the start rather than after its first increment.
func newRateLimitDecisions() *prometheus.CounterVec {
	vec := promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tasknexus_rate_limit_decisions_total",
		Help: "Number of rate limiter decisions by scope and result.",
	}, []string{"scope", "result"})
	for _, scope := range []string{ScopeAPI, ScopeAuth} {
		for _, result := range []string{ResultAllowed, ResultLimited, ResultError} {
			vec.WithLabelValues(scope, result)
		}
	}
	return vec
}

// RecordRateLimitDecision increments RateLimitDecisions for scope and result.
func RecordRateLimitDecision(scope, result string) {
	RateLimitDecisions.WithLabelValues(scope, result).Inc()
}
