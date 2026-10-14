package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
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
