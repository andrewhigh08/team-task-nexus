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

// Values of the result label of RateLimitDecisions.
const (
	ResultAllowed = "allowed"
	ResultLimited = "limited"
)

// ResultError is a value of the result label of both counters: in RateLimitDecisions it counts
// requests let through because the limiter could not decide, in CacheRequests cache operations
// that failed and were skipped.
const ResultError = "error"

// CacheTaskList is the cache label value of the team task list cache.
const CacheTaskList = "task_list"

// Values of the result label of CacheRequests, besides ResultError.
const (
	ResultHit  = "hit"
	ResultMiss = "miss"
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

// CacheRequests counts cache lookups by cache and result. Failed version reads, writes and
// invalidations count as errors too.
var CacheRequests = newCacheRequests()

// newCacheRequests registers the counter and creates every known series.
func newCacheRequests() *prometheus.CounterVec {
	vec := promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tasknexus_cache_requests_total",
		Help: "Number of cache requests by cache and result.",
	}, []string{"cache", "result"})
	for _, result := range []string{ResultHit, ResultMiss, ResultError} {
		vec.WithLabelValues(CacheTaskList, result)
	}
	return vec
}

// RecordCacheRequest increments CacheRequests for cache and result.
func RecordCacheRequest(cache, result string) {
	CacheRequests.WithLabelValues(cache, result).Inc()
}

// Values of the result label of OutboxEvents: the event was delivered, will be delivered
// again, or was given up on after its last attempt.
const (
	ResultSent   = "sent"
	ResultRetry  = "retry"
	ResultFailed = "failed"
)

// OutboxEvents counts the delivery attempts of outbox events by event type and result. Its
// series appear with the first delivery of each type: the event types belong to the domain,
// which this package does not depend on.
var OutboxEvents = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "tasknexus_outbox_events_total",
	Help: "Number of outbox event delivery attempts by event type and result.",
}, []string{"type", "result"})

// RecordOutboxEvent increments OutboxEvents for eventType and result.
func RecordOutboxEvent(eventType, result string) {
	OutboxEvents.WithLabelValues(eventType, result).Inc()
}
