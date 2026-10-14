// Package port defines the interfaces between the layers: the repositories and caches the
// services depend on, the outbox and event sender behind the delivery of events, the token
// verifier and rate limiter the HTTP middleware depends on, the services the HTTP handlers
// call, and the pingers behind the readiness probe.
package port

import (
	"context"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// UserRepository stores users. Lookups of a missing user fail with an apperror NOT_FOUND, so
// callers can tell an unknown user apart from a failing database.
type UserRepository interface {
	// Create inserts user and returns its id; a taken email fails with an apperror CONFLICT.
	Create(ctx context.Context, user *domain.User) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.User, error)
	// GetByEmail finds the user with the normalized email.
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

// TeamRepository stores teams, their members and the team analytics. Lookups of a missing
// team or membership fail with an apperror NOT_FOUND.
type TeamRepository interface {
	// Create inserts team and returns its id.
	Create(ctx context.Context, team *domain.Team) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Team, error)
	// ListForUser returns the teams userID is a member of, with the user's role, newest first.
	ListForUser(ctx context.Context, userID int64) ([]domain.TeamSummary, error)
	// AddMember inserts member; an existing membership fails with an apperror CONFLICT.
	AddMember(ctx context.Context, member domain.TeamMember) error
	// RemoveMember deletes the membership of userID in teamID.
	RemoveMember(ctx context.Context, teamID, userID int64) error
	// GetMember returns the membership of userID in teamID.
	GetMember(ctx context.Context, teamID, userID int64) (*domain.TeamMember, error)
	// ListMembers returns the members of teamID ordered by role (owner first), then by name.
	ListMembers(ctx context.Context, teamID int64) ([]domain.MemberProfile, error)
	// Stats summarizes every team userID is a member of, counting tasks completed at or after
	// since.
	Stats(ctx context.Context, userID int64, since time.Time) ([]domain.TeamStats, error)
	// TopContributors ranks the current members of teamID by the tasks they completed at or
	// after since and returns those ranked limit or better.
	TopContributors(ctx context.Context, teamID int64, since time.Time, limit int) ([]domain.TopContributor, error)
}

// TaskRepository stores tasks. Lookups of a missing task fail with an apperror NOT_FOUND.
type TaskRepository interface {
	// Create inserts task and returns its id.
	Create(ctx context.Context, task *domain.Task) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Task, error)
	// GetByIDForUpdate is GetByID with the row locked until the transaction ends; it must run
	// inside TxManager.WithinTx.
	GetByIDForUpdate(ctx context.Context, id int64) (*domain.Task, error)
	// Update writes every mutable field of task.
	Update(ctx context.Context, task *domain.Task) error
	// List returns the page of tasks q selects and the number of all matching tasks. Only tasks
	// of the teams q.ViewerID is a member of are ever selected.
	List(ctx context.Context, q domain.TaskQuery) ([]domain.Task, int, error)
	// ListOrphaned returns the tasks of teamID assigned to users who are no longer members.
	ListOrphaned(ctx context.Context, teamID int64) ([]domain.OrphanedAssignment, error)
}

// TaskHistoryRepository stores the change history of tasks.
type TaskHistoryRepository interface {
	// Add inserts changes, all with the same timestamp.
	Add(ctx context.Context, changes []domain.TaskChange) error
	// ListByTask returns the history of taskID, newest first.
	ListByTask(ctx context.Context, taskID int64) ([]domain.TaskChange, error)
}

// CommentRepository stores task comments. Lookups of a missing comment fail with an apperror
// NOT_FOUND.
type CommentRepository interface {
	// Create inserts comment and returns its id.
	Create(ctx context.Context, comment *domain.Comment) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Comment, error)
	// ListByTask returns the comments of taskID, oldest first.
	ListByTask(ctx context.Context, taskID int64) ([]domain.Comment, error)
}
