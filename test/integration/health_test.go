package integration

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbesMetricsAndContract(t *testing.T) {
	t.Parallel()
	c := newClient(t)

	live := c.do(t, http.MethodGet, "/health/live", nil, "")
	requireStatus(t, live, http.StatusOK)
	assert.JSONEq(t, `{"status":"ok"}`, string(live.Body))

	ready := c.do(t, http.MethodGet, "/health/ready", nil, "")
	requireStatus(t, ready, http.StatusOK)
	assert.JSONEq(t, `{"status":"ready","checks":{"mysql":"ok","redis":"ok"}}`, string(ready.Body))

	// Requests made so far show up in the metrics, under their route patterns.
	metrics := c.do(t, http.MethodGet, "/metrics", nil, "")
	requireStatus(t, metrics, http.StatusOK)
	assert.Contains(t, string(metrics.Body), `tasknexus_http_requests_total{method="GET",route="/health/ready",status="200"}`)
	assert.Contains(t, string(metrics.Body), "tasknexus_http_request_duration_seconds")
	assert.Contains(t, string(metrics.Body), "tasknexus_http_requests_in_flight")

	contract := c.do(t, http.MethodGet, "/openapi.yaml", nil, "")
	requireStatus(t, contract, http.StatusOK)
	assert.Contains(t, string(contract.Body), "openapi: 3.1")
	assert.Contains(t, string(contract.Body), "/api/v1/tasks/{id}/history:")
}

// TestReadinessFollowsTheDependencies boots a second instance on the databases of the suite,
// as another replica would: it finds the schema up to date and reports ready, and once its
// connections are closed it reports every dependency as unavailable.
func TestReadinessFollowsTheDependencies(t *testing.T) {
	t.Parallel()
	replica, c := bootApp(t, env(t).cfg)

	ready := c.do(t, http.MethodGet, "/health/ready", nil, "")
	requireStatus(t, ready, http.StatusOK)
	assert.JSONEq(t, `{"status":"ready","checks":{"mysql":"ok","redis":"ok"}}`, string(ready.Body))

	require.NoError(t, replica.Close())
	notReady := c.do(t, http.MethodGet, "/health/ready", nil, "")
	requireStatus(t, notReady, http.StatusServiceUnavailable)
	assert.JSONEq(t, `{"status":"not_ready","checks":{"mysql":"unavailable","redis":"unavailable"}}`, string(notReady.Body))
	// Liveness checks no dependency.
	requireStatus(t, c.do(t, http.MethodGet, "/health/live", nil, ""), http.StatusOK)
}

func TestResponsesCarryRequestIDsAndSecurityHeaders(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	acc := c.mustRegister(t, "Headers")
	generated := regexp.MustCompile(`^[0-9a-f]{32}$`)

	req := c.newRequest(t, http.MethodGet, "/api/v1/teams", nil, acc.Token)
	req.Header.Set("X-Request-ID", "client-id.42")
	api := c.send(t, req)
	requireStatus(t, api, http.StatusOK)
	assert.Equal(t, "client-id.42", api.Header.Get("X-Request-ID"), "a well-formed id is kept")
	assert.Equal(t, "nosniff", api.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", api.Header.Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", api.Header.Get("Referrer-Policy"))
	assert.Equal(t, "no-store", api.Header.Get("Cache-Control"))

	req = c.newRequest(t, http.MethodGet, "/health/live", nil, "")
	req.Header.Set("X-Request-ID", "not valid!")
	probe := c.send(t, req)
	assert.Regexp(t, generated, probe.Header.Get("X-Request-ID"), "a malformed id is replaced")
	assert.Equal(t, "nosniff", probe.Header.Get("X-Content-Type-Options"))
	assert.Empty(t, probe.Header.Get("Cache-Control"), "only API answers forbid caching")

	// Errors carry an id as well, and unknown routes answer with the error envelope.
	missing := c.do(t, http.MethodGet, "/api/v1/nowhere", nil, acc.Token)
	errBody := requireError(t, missing, http.StatusNotFound, "NOT_FOUND")
	assert.Equal(t, "route not found", errBody.Message)
	assert.Regexp(t, generated, missing.Header.Get("X-Request-ID"))
}
