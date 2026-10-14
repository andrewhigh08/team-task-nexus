package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The counters are process-wide and survive repeated runs, so every assertion is a delta.

func TestRateLimitDecisions_ExportsEverySeriesFromTheStart(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 6, testutil.CollectAndCount(RateLimitDecisions, "tasknexus_rate_limit_decisions_total"))
}

func TestRecordRateLimitDecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		scope  string
		result string
	}{
		{scope: ScopeAPI, result: ResultAllowed},
		{scope: ScopeAPI, result: ResultLimited},
		{scope: ScopeAPI, result: ResultError},
		{scope: ScopeAuth, result: ResultAllowed},
		{scope: ScopeAuth, result: ResultLimited},
		{scope: ScopeAuth, result: ResultError},
	}

	for _, tt := range tests {
		t.Run(tt.scope+"/"+tt.result, func(t *testing.T) {
			t.Parallel()
			before := testutil.ToFloat64(RateLimitDecisions.WithLabelValues(tt.scope, tt.result))

			RecordRateLimitDecision(tt.scope, tt.result)

			assert.InDelta(t, before+1, testutil.ToFloat64(RateLimitDecisions.WithLabelValues(tt.scope, tt.result)), 0)
		})
	}
}

func TestCacheRequests_ExportsEverySeriesFromTheStart(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3, testutil.CollectAndCount(CacheRequests, "tasknexus_cache_requests_total"))
}

func TestRecordCacheRequest(t *testing.T) {
	t.Parallel()
	for _, result := range []string{ResultHit, ResultMiss, ResultError} {
		t.Run(result, func(t *testing.T) {
			t.Parallel()
			before := testutil.ToFloat64(CacheRequests.WithLabelValues(CacheTaskList, result))

			RecordCacheRequest(CacheTaskList, result)

			assert.InDelta(t, before+1, testutil.ToFloat64(CacheRequests.WithLabelValues(CacheTaskList, result)), 0)
		})
	}
}

func TestRecordOutboxEvent(t *testing.T) {
	t.Parallel()
	for _, result := range []string{ResultSent, ResultRetry, ResultFailed} {
		t.Run(result, func(t *testing.T) {
			t.Parallel()
			const eventType = "metrics.test"
			before := testutil.ToFloat64(OutboxEvents.WithLabelValues(eventType, result))

			RecordOutboxEvent(eventType, result)

			assert.InDelta(t, before+1, testutil.ToFloat64(OutboxEvents.WithLabelValues(eventType, result)), 0)
		})
	}
}

// observations returns how many durations HTTPRequestDuration has observed for method and route.
func observations(t *testing.T, method, route string) uint64 {
	t.Helper()
	histogram, ok := HTTPRequestDuration.WithLabelValues(method, route).(prometheus.Histogram)
	require.True(t, ok)
	var m dto.Metric
	require.NoError(t, histogram.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestObserveHTTPRequest(t *testing.T) {
	t.Parallel()
	const route = "/metrics-test/{id}"
	before := testutil.ToFloat64(HTTPRequests.WithLabelValues("GET", route, "201"))
	observedBefore := observations(t, "GET", route)

	ObserveHTTPRequest("GET", route, 201, 250*time.Millisecond)

	assert.InDelta(t, before+1, testutil.ToFloat64(HTTPRequests.WithLabelValues("GET", route, "201")), 0)
	assert.Equal(t, observedBefore+1, observations(t, "GET", route))
}
