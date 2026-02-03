package http

import (
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shalfey088/team-task-nexus/internal/adapter/http/handler"
	"github.com/shalfey088/team-task-nexus/internal/adapter/http/middleware"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type RouterDeps struct {
	AuthHandler    *handler.AuthHandler
	TeamHandler    *handler.TeamHandler
	TaskHandler    *handler.TaskHandler
	CommentHandler *handler.CommentHandler
	HealthHandler  *handler.HealthHandler
	JWTSecret      string
	RateLimiter    port.RateLimiter
}

func NewRouter(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logging)
	r.Use(middleware.Metrics)

	r.Get("/health", deps.HealthHandler.Health)
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/register", deps.AuthHandler.Register)
		r.Post("/login", deps.AuthHandler.Login)

		r.Group(func(r chi.Router) {
			r.Use(middleware.JWTAuth(deps.JWTSecret))
			r.Use(middleware.RateLimit(deps.RateLimiter))

			r.Route("/teams", func(r chi.Router) {
				r.Post("/", deps.TeamHandler.Create)
				r.Get("/", deps.TeamHandler.List)
				r.Get("/stats", deps.TeamHandler.GetStats)
				r.Get("/{id}", deps.TeamHandler.GetByID)
				r.Post("/{id}/invite", deps.TeamHandler.Invite)
				r.Get("/{id}/top-contributors", deps.TeamHandler.GetTopContributors)
			})

			r.Route("/tasks", func(r chi.Router) {
				r.Post("/", deps.TaskHandler.Create)
				r.Get("/", deps.TaskHandler.List)
				r.Put("/{id}", deps.TaskHandler.Update)
				r.Get("/{id}/history", deps.TaskHandler.GetHistory)
				r.Get("/orphaned-assignees", deps.TaskHandler.GetOrphanedAssignees)

				r.Post("/{id}/comments", deps.CommentHandler.Create)
				r.Get("/{id}/comments", deps.CommentHandler.List)
			})
		})
	})

	return r
}
