package port

import (
	"context"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// RegisterInput is what a new account is created from. The service validates and normalizes it.
type RegisterInput struct {
	Email    string
	Password string
	FullName string
}

// LoginInput holds the credentials of a login attempt.
type LoginInput struct {
	Email    string
	Password string
}

// AuthResult is the outcome of a successful registration or login: a signed access token,
// the instant it expires and the account it was issued for.
type AuthResult struct {
	Token     string
	ExpiresAt time.Time
	User      domain.User
}

// AuthService registers accounts and exchanges credentials for access tokens.
type AuthService interface {
	// Register creates an account and issues a token for it. Invalid input fails with an
	// apperror VALIDATION_ERROR naming the fields, a taken email with CONFLICT.
	Register(ctx context.Context, in RegisterInput) (*AuthResult, error)
	// Login issues a token for valid credentials. An unknown email and a wrong password fail
	// with the same apperror UNAUTHORIZED.
	Login(ctx context.Context, in LoginInput) (*AuthResult, error)
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
