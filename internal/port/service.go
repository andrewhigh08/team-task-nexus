package port

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
)

type AuthService interface {
	Register(ctx context.Context, req domain.RegisterRequest) (*domain.AuthResponse, error)
	Login(ctx context.Context, req domain.LoginRequest) (*domain.AuthResponse, error)
}

type TeamService interface {
	Create(ctx context.Context, userID int64, req domain.CreateTeamRequest) (*domain.Team, error)
	GetByID(ctx context.Context, userID, teamID int64) (*domain.Team, error)
	ListByUserID(ctx context.Context, userID int64) ([]domain.Team, error)
	InviteUser(ctx context.Context, inviterID, teamID int64, req domain.InviteRequest) error
	GetStats(ctx context.Context, userID int64) ([]domain.TeamStats, error)
	GetTopContributors(ctx context.Context, userID, teamID int64) ([]domain.TopContributor, error)
}

type TaskService interface {
	Create(ctx context.Context, userID int64, req domain.CreateTaskRequest) (*domain.Task, error)
	Update(ctx context.Context, userID, taskID int64, req domain.UpdateTaskRequest) (*domain.Task, error)
	List(ctx context.Context, userID int64, filter domain.TaskFilter) (*domain.TaskListResponse, error)
	GetHistory(ctx context.Context, userID, taskID int64) ([]domain.TaskHistory, error)
	GetOrphanedAssignees(ctx context.Context) ([]domain.OrphanedAssignee, error)
}

type CommentService interface {
	Create(ctx context.Context, userID, taskID int64, req domain.CreateCommentRequest) (*domain.TaskComment, error)
	ListByTaskID(ctx context.Context, userID, taskID int64) ([]domain.TaskComment, error)
}

type NotificationService interface {
	NotifyTaskAssigned(ctx context.Context, task *domain.Task, assignee *domain.User) error
	NotifyCommentAdded(ctx context.Context, comment *domain.TaskComment, task *domain.Task) error
}
