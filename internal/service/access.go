package service

import (
	"context"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// The functions in this file enforce the team access rules: whoever is not a member of a team
// gets the same NOT_FOUND for the team and its tasks as for ones that do not exist, so ids
// cannot be probed; a member lacking the role an operation needs gets FORBIDDEN.

// requireMember returns the membership of userID in teamID, or NOT_FOUND for the team.
func requireMember(ctx context.Context, teams port.TeamRepository, teamID, userID int64) (*domain.TeamMember, error) {
	member, err := teams.GetMember(ctx, teamID, userID)
	if isNotFound(err) {
		return nil, apperror.NotFound("team", teamID)
	}
	if err != nil {
		return nil, err
	}
	return member, nil
}

// requireManager is requireMember for operations reserved to the owner and admins.
func requireManager(ctx context.Context, teams port.TeamRepository, teamID, userID int64) (*domain.TeamMember, error) {
	member, err := requireMember(ctx, teams, teamID, userID)
	if err != nil {
		return nil, err
	}
	if !member.Role.CanManageMembers() {
		return nil, errManagerRequired()
	}
	return member, nil
}

// requireTaskAccess checks that userID is a member of the team of task, answering NOT_FOUND
// for the task otherwise.
func requireTaskAccess(ctx context.Context, teams port.TeamRepository, task *domain.Task, userID int64) error {
	_, err := teams.GetMember(ctx, task.TeamID, userID)
	if isNotFound(err) {
		return apperror.NotFound("task", task.ID)
	}
	return err
}

// loadTask returns the task with taskID if userID may see it.
func loadTask(ctx context.Context, tasks port.TaskRepository, teams port.TeamRepository, taskID, userID int64) (*domain.Task, error) {
	task, err := tasks.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := requireTaskAccess(ctx, teams, task, userID); err != nil {
		return nil, err
	}
	return task, nil
}

// requireAssignable checks that assigneeID is a member of teamID; tasks may only be assigned
// within their team.
func requireAssignable(ctx context.Context, teams port.TeamRepository, teamID, assigneeID int64) error {
	_, err := teams.GetMember(ctx, teamID, assigneeID)
	if isNotFound(err) {
		errs := fieldErrors{}
		errs.add(fieldAssignee, "must be a member of the team")
		return errs.err()
	}
	return err
}
