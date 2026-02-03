package port

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type TeamRepository interface {
	Create(ctx context.Context, team *domain.Team) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Team, error)
	ListByUserID(ctx context.Context, userID int64) ([]domain.Team, error)
	AddMember(ctx context.Context, member *domain.TeamMember) error
	GetMember(ctx context.Context, teamID, userID int64) (*domain.TeamMember, error)
	GetStats(ctx context.Context, userID int64) ([]domain.TeamStats, error)
	GetTopContributors(ctx context.Context, teamID int64) ([]domain.TopContributor, error)
}

type TaskRepository interface {
	Create(ctx context.Context, task *domain.Task) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Task, error)
	Update(ctx context.Context, task *domain.Task) error
	List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int, error)
	GetOrphanedAssignees(ctx context.Context) ([]domain.OrphanedAssignee, error)
}

type TaskHistoryRepository interface {
	Create(ctx context.Context, history *domain.TaskHistory) error
	ListByTaskID(ctx context.Context, taskID int64) ([]domain.TaskHistory, error)
}

type CommentRepository interface {
	Create(ctx context.Context, comment *domain.TaskComment) (int64, error)
	ListByTaskID(ctx context.Context, taskID int64) ([]domain.TaskComment, error)
}

type TransactionManager interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
