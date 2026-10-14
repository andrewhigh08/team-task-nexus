package port

import (
	"context"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// TaskCache caches pages of a team's task list. Its keys do not include the viewer, so only
// queries narrowed to one team, whose members all see the same tasks, may be cached.
type TaskCache interface {
	// GetTaskList returns the cached page for q, or nil when there is none.
	GetTaskList(ctx context.Context, q domain.TaskQuery) (*domain.TaskPage, error)
	SetTaskList(ctx context.Context, q domain.TaskQuery, page *domain.TaskPage) error
	// InvalidateTeam drops every cached page of the team's tasks.
	InvalidateTeam(ctx context.Context, teamID int64) error
}

// RateLimiter limits the request rate of each user.
type RateLimiter interface {
	// Allow records a request of userID and reports whether it is within the limit.
	Allow(ctx context.Context, userID int64) (bool, error)
}
