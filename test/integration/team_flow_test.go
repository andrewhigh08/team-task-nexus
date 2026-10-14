//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

func TestTeamFlow_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	svc := newServices(t)

	// Register two users
	user1, err := svc.auth.Register(ctx, port.RegisterInput{
		Email: "owner@test.com", Password: "password", FullName: "Owner User",
	})
	require.NoError(t, err)
	require.NotEmpty(t, user1.Token)

	user2, err := svc.auth.Register(ctx, port.RegisterInput{
		Email: "member@test.com", Password: "password", FullName: "Member User",
	})
	require.NoError(t, err)

	// Create team
	team, err := svc.teams.Create(ctx, user1.User.ID, port.CreateTeamInput{
		Name: "Test Team", Description: "Integration test team",
	})
	require.NoError(t, err)
	assert.Equal(t, "Test Team", team.Name)

	// Add member
	member, err := svc.teams.AddMember(ctx, user1.User.ID, team.ID, port.AddMemberInput{
		Email: "member@test.com", Role: domain.TeamRoleMember,
	})
	require.NoError(t, err)
	assert.Equal(t, user2.User.ID, member.UserID)

	// List teams and members for user2
	teams, err := svc.teams.List(ctx, user2.User.ID)
	require.NoError(t, err)
	require.Len(t, teams, 1)
	assert.Equal(t, domain.TeamRoleMember, teams[0].Role)

	members, err := svc.teams.ListMembers(ctx, user2.User.ID, team.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, domain.TeamRoleOwner, members[0].Role)

	// Create task
	task, err := svc.tasks.Create(ctx, user1.User.ID, port.CreateTaskInput{
		Title: "Integration Test Task", TeamID: team.ID, Priority: new(domain.TaskPriorityMedium),
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusTodo, task.Status)

	// Update task
	updated, err := svc.tasks.Update(ctx, user1.User.ID, task.ID, port.UpdateTaskInput{
		Status: opt.Of(domain.TaskStatusInProgress),
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusInProgress, updated.Status)

	// Check history
	history, err := svc.tasks.History(ctx, user1.User.ID, task.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(history), 1)

	// Add comment
	comment, err := svc.comments.Create(ctx, user2.User.ID, task.ID, port.CreateCommentInput{
		Content: "Working on this task!",
	})
	require.NoError(t, err)
	assert.Equal(t, "Working on this task!", comment.Content)

	// List comments
	comments, err := svc.comments.List(ctx, user2.User.ID, task.ID)
	require.NoError(t, err)
	assert.Len(t, comments, 1)

	// Complete the task as its assignee
	done, err := svc.tasks.Update(ctx, user2.User.ID, task.ID, port.UpdateTaskInput{
		AssigneeID: opt.Of(user2.User.ID),
		Status:     opt.Of(domain.TaskStatusDone),
	})
	require.NoError(t, err)
	assert.NotNil(t, done.CompletedAt)

	// Get team stats
	stats, err := svc.teams.Stats(ctx, user1.User.ID)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, domain.TeamStats{TeamID: team.ID, Name: "Test Team", MemberCount: 2, OpenTasks: 0, DoneLast7Days: 1}, stats[0])

	// Get top contributors
	contributors, err := svc.teams.TopContributors(ctx, user1.User.ID, team.ID)
	require.NoError(t, err)
	assert.Equal(t, []domain.TopContributor{{UserID: user2.User.ID, FullName: "Member User", TasksCompleted: 1, Rank: 1}}, contributors)
}
