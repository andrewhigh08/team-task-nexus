package port

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
)

type TaskCache interface {
	GetTaskList(ctx context.Context, filter domain.TaskFilter) (*domain.TaskListResponse, error)
	SetTaskList(ctx context.Context, filter domain.TaskFilter, response *domain.TaskListResponse) error
	InvalidateTeam(ctx context.Context, teamID int64) error
}

type RateLimiter interface {
	Allow(ctx context.Context, userID int64) (bool, error)
}
