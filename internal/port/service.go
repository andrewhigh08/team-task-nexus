package port

import (
	"context"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
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

// CreateTeamInput is what a new team is created from.
type CreateTeamInput struct {
	Name        string
	Description string
}

// AddMemberInput names the registered user to add to a team and the role to give them. An
// empty Role means domain.TeamRoleMember.
type AddMemberInput struct {
	Email string
	Role  domain.TeamRole
}

// TeamService manages teams and their members. A team the caller is not a member of fails
// with an apperror NOT_FOUND, exactly like a team that does not exist; a member lacking the
// role an operation needs gets FORBIDDEN. Invalid input fails with VALIDATION_ERROR.
type TeamService interface {
	// Create creates a team owned by userID.
	Create(ctx context.Context, userID int64, in CreateTeamInput) (*domain.Team, error)
	Get(ctx context.Context, userID, teamID int64) (*domain.Team, error)
	// List returns the teams of userID with the user's role in each.
	List(ctx context.Context, userID int64) ([]domain.TeamSummary, error)
	ListMembers(ctx context.Context, userID, teamID int64) ([]domain.MemberProfile, error)
	// AddMember adds a registered user to the team; only the owner and admins may add
	// members, and only the owner may add admins. An unknown email fails with NOT_FOUND, an
	// existing member with CONFLICT.
	AddMember(ctx context.Context, userID, teamID int64, in AddMemberInput) (*domain.MemberProfile, error)
	// RemoveMember removes memberID from the team. The owner may remove anyone but
	// themselves, an admin only plain members, and anyone but the owner may leave.
	RemoveMember(ctx context.Context, userID, teamID, memberID int64) error
	// Stats summarizes every team of userID.
	Stats(ctx context.Context, userID int64) ([]domain.TeamStats, error)
	// TopContributors ranks the members of the team by the tasks they recently completed.
	TopContributors(ctx context.Context, userID, teamID int64) ([]domain.TopContributor, error)
}

// CreateTaskInput is what a new task is created from. A nil Priority means
// domain.TaskPriorityMedium; DueDate is a calendar date at midnight UTC.
type CreateTaskInput struct {
	TeamID      int64
	Title       string
	Description string
	Priority    *domain.TaskPriority
	AssigneeID  *int64
	DueDate     *time.Time
}

// UpdateTaskInput is a partial update of a task: absent fields keep their value. AssigneeID
// and DueDate may be null to clear them; the other fields cannot be null.
type UpdateTaskInput struct {
	Title       opt.Field[string]
	Description opt.Field[string]
	Status      opt.Field[domain.TaskStatus]
	Priority    opt.Field[domain.TaskPriority]
	AssigneeID  opt.Field[int64]
	DueDate     opt.Field[time.Time]
}

// TaskService manages tasks. A task of a team the caller is not a member of fails with an
// apperror NOT_FOUND, exactly like a task that does not exist. Invalid input fails with
// VALIDATION_ERROR; an assignee must be a member of the task's team.
type TaskService interface {
	// Create creates a task in in.TeamID, created by userID.
	Create(ctx context.Context, userID int64, in CreateTaskInput) (*domain.Task, error)
	Get(ctx context.Context, userID, taskID int64) (*domain.Task, error)
	// Update applies in to the task and records every changed field in its history. An
	// update that changes nothing writes nothing.
	Update(ctx context.Context, userID, taskID int64, in UpdateTaskInput) (*domain.Task, error)
	// List returns a page of the tasks q selects; q.ViewerID is the caller, and only tasks of
	// the caller's teams are listed. Zero Page and PageSize mean the first page of 20.
	List(ctx context.Context, q domain.TaskQuery) (*domain.TaskPage, error)
	// History returns page p of the change history of the task, newest first. Zero Page and
	// PageSize mean the first page of 20.
	History(ctx context.Context, userID, taskID int64, p domain.PageRequest) (*domain.TaskChangePage, error)
	// ListOrphaned returns the tasks of the team assigned to users who have left it; only the
	// owner and admins may see them.
	ListOrphaned(ctx context.Context, userID, teamID int64) ([]domain.OrphanedAssignment, error)
}

// CreateCommentInput is what a new comment is created from.
type CreateCommentInput struct {
	Content string
}

// CommentService manages task comments, with the same access rules as TaskService.
type CommentService interface {
	// Create adds a comment by userID to the task.
	Create(ctx context.Context, userID, taskID int64, in CreateCommentInput) (*domain.Comment, error)
	// List returns page p of the comments of the task, oldest first. Zero Page and PageSize
	// mean the first page of 20.
	List(ctx context.Context, userID, taskID int64, p domain.PageRequest) (*domain.CommentPage, error)
}
