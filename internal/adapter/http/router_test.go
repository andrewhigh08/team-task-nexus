package http_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/api"
	apphttp "github.com/andrewhigh08/team-task-nexus/internal/adapter/http"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/handler"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const (
	testAPIRateLimit  = 100
	testAuthRateLimit = 10
	testToken         = "header.payload.signature"
	testUserID        = int64(7)
	clientAddr        = "203.0.113.7"
)

// publicRoutes are served to anyone, without authentication or rate limits.
var publicRoutes = map[string]bool{
	"GET /health/live":  true,
	"GET /health/ready": true,
	"GET /metrics":      true,
	"GET /openapi.yaml": true,
}

// authRoutes are limited per client address instead of per user, since they have no user yet.
var authRoutes = map[string]bool{
	"POST /api/v1/register": true,
	"POST /api/v1/login":    true,
}

// pingerFunc turns a function into a port.Pinger.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

// testRouter is a router on mocks, with the log output it produced.
type testRouter struct {
	*chi.Mux
	limiter  *mocks.MockRateLimiter
	verifier *mocks.MockTokenVerifier
	teams    *mocks.MockTeamService
	logs     *bytes.Buffer
}

// newTestRouter returns the router on mock services, a mock limiter and verifier, and probes
// whose dependencies answer readyErr.
func newTestRouter(t *testing.T, trustProxy bool, readyErr error) *testRouter {
	t.Helper()
	ctrl := gomock.NewController(t)
	tr := &testRouter{
		limiter:  mocks.NewMockRateLimiter(ctrl),
		verifier: mocks.NewMockTokenVerifier(ctrl),
		teams:    mocks.NewMockTeamService(ctrl),
		logs:     &bytes.Buffer{},
	}
	ping := pingerFunc(func(context.Context) error { return readyErr })
	tr.Mux = apphttp.NewRouter(apphttp.RouterDeps{
		AuthHandler:       handler.NewAuthHandler(mocks.NewMockAuthService(ctrl)),
		TeamHandler:       handler.NewTeamHandler(tr.teams),
		TaskHandler:       handler.NewTaskHandler(mocks.NewMockTaskService(ctrl)),
		CommentHandler:    handler.NewCommentHandler(mocks.NewMockCommentService(ctrl)),
		HealthHandler:     handler.NewHealthHandler(map[string]port.Pinger{"mysql": ping, "redis": ping}),
		TokenVerifier:     tr.verifier,
		RateLimiter:       tr.limiter,
		Logger:            logger.New(logger.Config{Level: "debug", Format: "json", Output: tr.logs}),
		APIRateLimit:      testAPIRateLimit,
		AuthRateLimit:     testAuthRateLimit,
		TrustProxyHeaders: trustProxy,
	})
	return tr
}

// newRequest returns a request from clientAddr; a non-empty token is sent as bearer token.
func newRequest(t *testing.T, method, target, token string, body io.Reader) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, body)
	req.RemoteAddr = clientAddr + ":4711"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// servedRoutes returns every route of r as "METHOD pattern".
func servedRoutes(t *testing.T, r chi.Routes) []string {
	t.Helper()
	var routes []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	require.NoError(t, err)
	return routes
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// targetOf returns a request target for a route: method and path with every parameter set to 1.
func targetOf(route string) (method, target string) {
	method, pattern, _ := strings.Cut(route, " ")
	return method, pathParam.ReplaceAllString(pattern, "1")
}

func TestNewRouter_GuardsEveryRoute(t *testing.T) {
	t.Parallel()
	routes := servedRoutes(t, newTestRouter(t, false, nil))
	require.NotEmpty(t, routes)

	for _, route := range routes {
		method, target := targetOf(route)
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			switch {
			case publicRoutes[route]:
				// The mocks expect no call: neither the verifier nor the limiter is consulted.
				rec := serve(newTestRouter(t, false, nil), newRequest(t, method, target, "", http.NoBody))

				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Empty(t, rec.Header().Get("X-RateLimit-Limit"))
			case authRoutes[route]:
				tr := newTestRouter(t, false, nil)
				tr.limiter.EXPECT().Allow(gomock.Any(), "rl:auth:ip:"+clientAddr, testAuthRateLimit, time.Minute).
					Return(port.RateDecision{RetryAfter: time.Second}, nil)

				rec := serve(tr, newRequest(t, method, target, "", http.NoBody))

				assert.Equal(t, http.StatusTooManyRequests, rec.Code, "limited per client address")
			default:
				anonymous := serve(newTestRouter(t, false, nil), newRequest(t, method, target, "", http.NoBody))
				assert.Equal(t, http.StatusUnauthorized, anonymous.Code, "authentication is required")

				tr := newTestRouter(t, false, nil)
				tr.verifier.EXPECT().Verify(testToken).Return(testUserID, nil)
				tr.limiter.EXPECT().Allow(gomock.Any(), "rl:api:user:7", testAPIRateLimit, time.Minute).
					Return(port.RateDecision{RetryAfter: time.Second}, nil)

				authenticated := serve(tr, newRequest(t, method, target, testToken, http.NoBody))

				assert.Equal(t, http.StatusTooManyRequests, authenticated.Code, "limited per user")
			}
		})
	}
}

func TestNewRouter_KeysTheAuthLimitByTheClientAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		trustProxy bool
		wantKey    string
	}{
		{name: "forwarding headers are ignored by default", wantKey: "rl:auth:ip:" + clientAddr},
		{name: "behind a trusted proxy", trustProxy: true, wantKey: "rl:auth:ip:198.51.100.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTestRouter(t, tt.trustProxy, nil)
			tr.limiter.EXPECT().Allow(gomock.Any(), tt.wantKey, testAuthRateLimit, time.Minute).
				Return(port.RateDecision{RetryAfter: 30 * time.Second}, nil)
			req := newRequest(t, http.MethodPost, "/api/v1/login", "", http.NoBody)
			req.Header.Set("X-Forwarded-For", "198.51.100.1")

			rec := serve(tr, req)

			assert.Equal(t, http.StatusTooManyRequests, rec.Code)
			assert.Equal(t, "30", rec.Header().Get("Retry-After"))
		})
	}
}

func TestNewRouter_AppliesTheCommonMiddleware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		target      string
		wantStatus  int
		wantNoStore bool
	}{
		{name: "api", target: "/api/v1/teams", wantStatus: http.StatusUnauthorized, wantNoStore: true},
		{name: "probe", target: "/health/live", wantStatus: http.StatusOK},
		{name: "unmatched", target: "/nothing/here", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newTestRouter(t, false, nil)
			req := newRequest(t, http.MethodGet, tt.target, "", http.NoBody)
			req.Header.Set("X-Request-ID", "req-"+tt.name)

			rec := serve(tr, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "req-"+tt.name, rec.Header().Get("X-Request-ID"))
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
			if tt.wantNoStore {
				assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			}
			assert.Contains(t, tr.logs.String(), `"request_id":"req-`+tt.name+`"`, "the request is logged")
		})
	}
}

func TestNewRouter_AnswersUnknownPathsWithTheEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
	}{
		{name: "unknown path", target: "/api/v1/nope"},
		{name: "trailing slash", target: "/api/v1/teams/"},
		{name: "outside the api", target: "/nothing/here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(newTestRouter(t, false, nil), newRequest(t, http.MethodGet, tt.target, "", http.NoBody))

			assert.Equal(t, http.StatusNotFound, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"success":false,"error":{"code":"NOT_FOUND","message":"route not found",`+
				`"details":{"resource":"route","id":"`+tt.target+`"}}}`, rec.Body.String())
		})
	}
}

func TestNewRouter_RecoversFromAPanic(t *testing.T) {
	t.Parallel()
	tr := newTestRouter(t, false, nil)
	tr.verifier.EXPECT().Verify(testToken).Return(testUserID, nil)
	tr.limiter.EXPECT().Allow(gomock.Any(), "rl:api:user:7", testAPIRateLimit, time.Minute).
		Return(port.RateDecision{Allowed: true, Remaining: 99}, nil)
	tr.teams.EXPECT().List(gomock.Any(), testUserID).DoAndReturn(func(context.Context, int64) ([]domain.TeamSummary, error) {
		panic("nil map")
	})

	rec := serve(tr, newRequest(t, http.MethodGet, "/api/v1/teams", testToken, http.NoBody))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`, rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
	assert.Contains(t, tr.logs.String(), `"msg":"panic recovered"`)
}

func TestNewRouter_LimitsTheBodySize(t *testing.T) {
	t.Parallel()
	tr := newTestRouter(t, false, nil)
	tr.limiter.EXPECT().Allow(gomock.Any(), gomock.Any(), testAuthRateLimit, time.Minute).
		Return(port.RateDecision{Allowed: true, Remaining: 9}, nil)
	body := `{"email":"alice@example.com","password":"` + strings.Repeat("p", 1<<20) + `"}`
	req := newRequest(t, http.MethodPost, "/api/v1/register", "", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := serve(tr, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"PAYLOAD_TOO_LARGE","message":"request body is too large",`+
		`"details":{"limit_bytes":1048576}}}`, rec.Body.String())
}

func TestNewRouter_ServesTheProbes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		target     string
		readyErr   error
		wantStatus int
		wantBody   string
	}{
		{name: "alive", target: "/health/live", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		{name: "ready", target: "/health/ready", wantStatus: http.StatusOK, wantBody: `{"status":"ready","checks":{"mysql":"ok","redis":"ok"}}`},
		{
			name: "not ready", target: "/health/ready", readyErr: context.DeadlineExceeded, wantStatus: http.StatusServiceUnavailable,
			wantBody: `{"status":"not_ready","checks":{"mysql":"unavailable","redis":"unavailable"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(newTestRouter(t, false, tt.readyErr), newRequest(t, http.MethodGet, tt.target, "", http.NoBody))

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestNewRouter_ServesTheMetrics(t *testing.T) {
	t.Parallel()
	tr := newTestRouter(t, false, nil)
	serve(tr, newRequest(t, http.MethodGet, "/health/live", "", http.NoBody))

	rec := serve(tr, newRequest(t, http.MethodGet, "/metrics", "", http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `tasknexus_http_requests_total{method="GET",route="/health/live",status="200"}`)
}

func TestNewRouter_ServesTheOpenAPIDocument(t *testing.T) {
	t.Parallel()

	rec := serve(newTestRouter(t, false, nil), newRequest(t, http.MethodGet, "/openapi.yaml", "", http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, api.OpenAPI, rec.Body.String())
}
