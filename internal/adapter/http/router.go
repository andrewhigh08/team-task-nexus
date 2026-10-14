// Package http assembles the HTTP API: the router with its middleware and routes.
package http

import (
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/handler"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

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

// NewRouter returns the router of the API.
func NewRouter(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.Logging(deps.Logger))
	r.Use(middleware.Metrics)

	r.Get("/health", deps.HealthHandler.Health)
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimitByIP(deps.RateLimiter, deps.AuthRateLimit, deps.TrustProxyHeaders))
			r.Post("/register", deps.AuthHandler.Register)
			r.Post("/login", deps.AuthHandler.Login)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(deps.TokenVerifier))
			r.Use(middleware.RateLimitByUser(deps.RateLimiter, deps.APIRateLimit))

			r.Route("/teams", func(r chi.Router) {
				r.Post("/", deps.TeamHandler.Create)
				r.Get("/", deps.TeamHandler.List)
				r.Get("/stats", deps.TeamHandler.Stats)
				r.Get("/{id}", deps.TeamHandler.Get)
				r.Get("/{id}/members", deps.TeamHandler.ListMembers)
				r.Post("/{id}/members", deps.TeamHandler.AddMember)
				r.Delete("/{id}/members/{user_id}", deps.TeamHandler.RemoveMember)
				r.Get("/{id}/top-contributors", deps.TeamHandler.TopContributors)
				r.Get("/{id}/orphaned-assignees", deps.TaskHandler.ListOrphaned)
			})

			r.Route("/tasks", func(r chi.Router) {
				r.Post("/", deps.TaskHandler.Create)
				r.Get("/", deps.TaskHandler.List)
				r.Get("/{id}", deps.TaskHandler.Get)
				r.Patch("/{id}", deps.TaskHandler.Update)
				r.Get("/{id}/history", deps.TaskHandler.History)
				r.Post("/{id}/comments", deps.CommentHandler.Create)
				r.Get("/{id}/comments", deps.CommentHandler.List)
			})
		})
	})

	return r
}
