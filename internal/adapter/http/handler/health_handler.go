package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// pingTimeout bounds every readiness check, so that a dependency that hangs fails the probe
// instead of holding it open.
const pingTimeout = 2 * time.Second

// Values of the probe bodies.
const (
	statusOK       = "ok"
	statusReady    = "ready"
	statusNotReady = "not_ready"

	checkOK          = "ok"
	checkUnavailable = "unavailable"
)

type liveResponse struct {
	Status string `json:"status"`
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// HealthHandler serves the liveness and readiness probes.
type HealthHandler struct {
	checks map[string]port.Pinger
}

// NewHealthHandler returns a HealthHandler whose readiness probe pings every dependency in
// checks and reports it under its key.
func NewHealthHandler(checks map[string]port.Pinger) *HealthHandler {
	return &HealthHandler{checks: checks}
}

// Live answers 200 for as long as the process serves requests. It checks no dependency: an
// outage of MySQL or Redis is no reason to restart the process.
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	response.Plain(w, http.StatusOK, liveResponse{Status: statusOK})
}

// Ready answers 200 when every dependency answers its ping and 503 when one does not, with
// the outcome of each check.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := h.ping(r.Context())
	status, code := statusReady, http.StatusOK
	for _, outcome := range checks {
		if outcome != checkOK {
			status, code = statusNotReady, http.StatusServiceUnavailable
		}
	}
	response.Plain(w, code, readyResponse{Status: status, Checks: checks})
}

// ping runs the checks concurrently, each within pingTimeout, so the probe takes no longer
// than the slowest of them. A failure is logged with its cause; the probe is public, so its
// body only says that the dependency is unavailable.
func (h *HealthHandler) ping(ctx context.Context) map[string]string {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		outcomes = make(map[string]string, len(h.checks))
	)
	for name, pinger := range h.checks {
		wg.Go(func() {
			outcome := checkOK
			if err := pingWithin(ctx, pinger); err != nil {
				logger.FromContext(ctx).Warn("readiness check failed", "check", name, "error", err)
				outcome = checkUnavailable
			}
			mu.Lock()
			defer mu.Unlock()
			outcomes[name] = outcome
		})
	}
	wg.Wait()
	return outcomes
}

func pingWithin(ctx context.Context, pinger port.Pinger) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return pinger.Ping(ctx)
}
