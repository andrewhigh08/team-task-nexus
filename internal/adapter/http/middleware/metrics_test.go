package middleware_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
)

// The collectors are process-wide, so these tests assert deltas and do not run in parallel:
// another test serving requests at the same time would skew the in-flight gauge.

// metricsRouter serves /metrics-test/{id} behind Metrics, after Recoverer so that panics are
// answered.
func metricsRouter(h http.HandlerFunc) http.Handler {
	r := chi.NewRouter()
	var logs bytes.Buffer
	r.Use(middleware.Recoverer(newTestLogger(&logs)), middleware.Metrics)
	r.Get("/metrics-test/{id}", h)
	return r
}

func requestCount(method, route string, status int) float64 {
	return testutil.ToFloat64(metrics.HTTPRequests.WithLabelValues(method, route, strconv.Itoa(status)))
}

// observations returns how many durations have been observed for method and route.
func observations(t *testing.T, method, route string) uint64 {
	t.Helper()
	histogram, ok := metrics.HTTPRequestDuration.WithLabelValues(method, route).(prometheus.Histogram)
	require.True(t, ok)
	var m dto.Metric
	require.NoError(t, histogram.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestMetrics_RecordsTheRequest(t *testing.T) {
	var inFlightWhileServed float64
	h := metricsRouter(func(w http.ResponseWriter, _ *http.Request) {
		inFlightWhileServed = testutil.ToFloat64(metrics.HTTPRequestsInFlight)
		w.WriteHeader(http.StatusAccepted)
	})
	countBefore := requestCount(http.MethodGet, "/metrics-test/{id}", http.StatusAccepted)
	observedBefore := observations(t, http.MethodGet, "/metrics-test/{id}")
	inFlightBefore := testutil.ToFloat64(metrics.HTTPRequestsInFlight)

	rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics-test/42", http.NoBody))

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.InDelta(t, countBefore+1, requestCount(http.MethodGet, "/metrics-test/{id}", http.StatusAccepted), 0)
	assert.Zero(t, requestCount(http.MethodGet, "/metrics-test/42", http.StatusAccepted), "the raw path is never a label")
	assert.Equal(t, observedBefore+1, observations(t, http.MethodGet, "/metrics-test/{id}"), "the duration is observed under the route")
	assert.InDelta(t, inFlightBefore+1, inFlightWhileServed, 0)
	assert.InDelta(t, inFlightBefore, testutil.ToFloat64(metrics.HTTPRequestsInFlight), 0)
}

func TestMetrics_BoundsTheLabels(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantMethod string
		wantRoute  string
		wantStatus int
	}{
		{
			name: "unmatched path", method: http.MethodGet, target: "/no/such/route",
			wantMethod: http.MethodGet, wantRoute: metrics.RouteUnmatched, wantStatus: http.StatusNotFound,
		},
		{
			name: "method not allowed", method: http.MethodDelete, target: "/metrics-test/42",
			wantMethod: http.MethodDelete, wantRoute: metrics.RouteUnmatched, wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "made-up method", method: "BREW", target: "/metrics-test/42",
			wantMethod: metrics.MethodOther, wantRoute: metrics.RouteUnmatched, wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := metricsRouter(noContent)
			before := requestCount(tt.wantMethod, tt.wantRoute, tt.wantStatus)

			rec := serve(h, httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, http.NoBody))

			require.Equal(t, tt.wantStatus, rec.Code)
			assert.InDelta(t, before+1, requestCount(tt.wantMethod, tt.wantRoute, tt.wantStatus), 0)
		})
	}
}

func TestMetrics_CountsAPanickingRequestAs500(t *testing.T) {
	h := metricsRouter(panicking("boom"))
	before := requestCount(http.MethodGet, "/metrics-test/{id}", http.StatusInternalServerError)
	inFlightBefore := testutil.ToFloat64(metrics.HTTPRequestsInFlight)

	rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics-test/1", http.NoBody))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.InDelta(t, before+1, requestCount(http.MethodGet, "/metrics-test/{id}", http.StatusInternalServerError), 0)
	assert.InDelta(t, inFlightBefore, testutil.ToFloat64(metrics.HTTPRequestsInFlight), 0, "the request leaves the gauge")
}
