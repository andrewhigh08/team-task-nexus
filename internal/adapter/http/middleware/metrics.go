package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
)

// Metrics records every request in the HTTP metrics: it is counted in flight while it is
// served, then by method, route and status, and its duration is observed by method and route.
// The route is the pattern chi matched, so the number of series stays bounded whatever paths
// and methods clients send.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		metrics.HTTPRequestsInFlight.Inc()
		rec := newResponseRecorder(w)
		completed := false
		// Deferred, so that a request whose handler panics is counted too.
		defer func() {
			metrics.HTTPRequestsInFlight.Dec()
			metrics.ObserveHTTPRequest(methodLabel(r.Method), routeLabel(r), rec.finalStatus(completed), time.Since(start))
		}()
		next.ServeHTTP(rec, r)
		completed = true
	})
}

// routeLabel returns the pattern of the route chi matched r with, or metrics.RouteUnmatched.
// chi fills the pattern in while routing, so it is complete once the request has been served.
func routeLabel(r *http.Request) string {
	if pattern := chi.RouteContext(r.Context()).RoutePattern(); pattern != "" {
		return pattern
	}
	return metrics.RouteUnmatched
}

// methodLabel returns method if it is a standard one and metrics.MethodOther otherwise.
func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return metrics.MethodOther
	}
}
