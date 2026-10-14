// Package http assembles the HTTP API: the router with its middleware and routes.
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/andrewhigh08/team-task-nexus/api"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/handler"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// maxBodyBytes caps request bodies. The largest valid body, a task with a description of
// 10000 characters, stays far below it.
const maxBodyBytes = 1 << 20

// RouterDeps holds what the router serves requests with.
type RouterDeps struct {
	AuthHandler    *handler.AuthHandler
	TeamHandler    *handler.TeamHandler
	TaskHandler    *handler.TaskHandler
	CommentHandler *handler.CommentHandler
	HealthHandler  *handler.HealthHandler
	TokenVerifier  port.TokenVerifier
	RateLimiter    port.RateLimiter
	Logger         *logger.Logger

	// APIRateLimit is the number of requests per minute admitted for each authenticated user.
	APIRateLimit int
	// AuthRateLimit is the number of registration and login requests per minute admitted for
	// each client address.
	AuthRateLimit int
	// TrustProxyHeaders takes client addresses from X-Forwarded-For and X-Real-IP.
	TrustProxyHeaders bool
}

// NewRouter returns the router of the API. Every route is registered with its full path, so
// the patterns in logs and metrics are exactly the paths of the OpenAPI document.
func NewRouter(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	r.Use(
		middleware.RequestID,
		middleware.Recoverer(deps.Logger),
		middleware.AccessLog(deps.Logger),
		middleware.Metrics,
		middleware.SecurityHeaders,
		middleware.BodyLimit(maxBodyBytes),
	)
	r.NotFound(notFound)

	r.Get("/health/live", deps.HealthHandler.Live)
	r.Get("/health/ready", deps.HealthHandler.Ready)
	r.Method(http.MethodGet, "/metrics", promhttp.Handler())
	r.Get("/openapi.yaml", handler.OpenAPI(api.OpenAPI))

	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimitByIP(deps.RateLimiter, deps.AuthRateLimit, deps.TrustProxyHeaders))

		r.Post("/api/v1/register", deps.AuthHandler.Register)
		r.Post("/api/v1/login", deps.AuthHandler.Login)
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate(deps.TokenVerifier))
		r.Use(middleware.RateLimitByUser(deps.RateLimiter, deps.APIRateLimit))

		r.Post("/api/v1/teams", deps.TeamHandler.Create)
		r.Get("/api/v1/teams", deps.TeamHandler.List)
		r.Get("/api/v1/teams/stats", deps.TeamHandler.Stats)
		r.Get("/api/v1/teams/{id}", deps.TeamHandler.Get)
		r.Get("/api/v1/teams/{id}/members", deps.TeamHandler.ListMembers)
		r.Post("/api/v1/teams/{id}/members", deps.TeamHandler.AddMember)
		r.Delete("/api/v1/teams/{id}/members/{user_id}", deps.TeamHandler.RemoveMember)
		r.Get("/api/v1/teams/{id}/top-contributors", deps.TeamHandler.TopContributors)
		r.Get("/api/v1/teams/{id}/orphaned-assignees", deps.TaskHandler.ListOrphaned)

		r.Post("/api/v1/tasks", deps.TaskHandler.Create)
		r.Get("/api/v1/tasks", deps.TaskHandler.List)
		r.Get("/api/v1/tasks/{id}", deps.TaskHandler.Get)
		r.Patch("/api/v1/tasks/{id}", deps.TaskHandler.Update)
		r.Get("/api/v1/tasks/{id}/history", deps.TaskHandler.History)
		r.Post("/api/v1/tasks/{id}/comments", deps.CommentHandler.Create)
		r.Get("/api/v1/tasks/{id}/comments", deps.CommentHandler.List)
	})

	return r
}

// notFound answers a path no route matches with the error envelope, so that a client of the
// API gets JSON even for a mistyped path; chi would answer in plain text.
func notFound(w http.ResponseWriter, r *http.Request) {
	response.Error(w, r, apperror.NotFound("route", r.URL.Path))
}
