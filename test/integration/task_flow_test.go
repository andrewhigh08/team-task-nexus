//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

func TestTaskCaching_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	svc := newServices(t)

	// Setup
	user, err := svc.auth.Register(ctx, port.RegisterInput{
		Email: "cache-test@test.com", Password: "password", FullName: "Cache User",
	})
	require.NoError(t, err)

	team, err := svc.teams.Create(ctx, user.User.ID, port.CreateTeamInput{
		Name: "Cache Team",
	})
	require.NoError(t, err)

	// Create tasks
	for i := range 5 {
		_, err := svc.tasks.Create(ctx, user.User.ID, port.CreateTaskInput{
			Title:  "Task " + string(rune('A'+i)),
			TeamID: team.ID,
		})
		require.NoError(t, err)
	}

	// First list - should hit DB and cache result
	query := domain.TaskQuery{ViewerID: user.User.ID, TeamID: team.ID, Page: 1, PageSize: 20}
	result1, err := svc.tasks.List(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, 5, result1.Total)

	// Second list - should hit cache
	result2, err := svc.tasks.List(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, 5, result2.Total)
	assert.Equal(t, result1.Total, result2.Total)

	// Check that data is in Redis
	cached, err := svc.cache.GetTaskList(ctx, query)
	require.NoError(t, err)
	assert.NotNil(t, cached)
	assert.Equal(t, 5, cached.Total)
}

func TestTaskPagination_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	svc := newServices(t)

	user, err := svc.auth.Register(ctx, port.RegisterInput{
		Email: "paging@test.com", Password: "password", FullName: "Paging User",
	})
	require.NoError(t, err)

	team, err := svc.teams.Create(ctx, user.User.ID, port.CreateTeamInput{
		Name: "Paging Team",
	})
	require.NoError(t, err)

	for range 15 {
		_, err := svc.tasks.Create(ctx, user.User.ID, port.CreateTaskInput{
			Title:  "Paginated Task",
			TeamID: team.ID,
		})
		require.NoError(t, err)
	}

	// Page 1
	result, err := svc.tasks.List(ctx, domain.TaskQuery{
		ViewerID: user.User.ID, TeamID: team.ID, Page: 1, PageSize: 5,
	})
	require.NoError(t, err)
	assert.Len(t, result.Tasks, 5)
	assert.Equal(t, 15, result.Total)
	assert.Equal(t, 3, result.TotalPages)

	// Page 2
	result2, err := svc.tasks.List(ctx, domain.TaskQuery{
		ViewerID: user.User.ID, TeamID: team.ID, Page: 2, PageSize: 5,
	})
	require.NoError(t, err)
	assert.Len(t, result2.Tasks, 5)
}

func TestOrphanedAssignees_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	svc := newServices(t)

	user1, err := svc.auth.Register(ctx, port.RegisterInput{
		Email: "orphan-owner@test.com", Password: "password", FullName: "Owner",
	})
	require.NoError(t, err)
	user2, err := svc.auth.Register(ctx, port.RegisterInput{
		Email: "orphan-member@test.com", Password: "password", FullName: "Leaving Member",
	})
	require.NoError(t, err)

	team, err := svc.teams.Create(ctx, user1.User.ID, port.CreateTeamInput{
		Name: "Orphan Team",
	})
	require.NoError(t, err)
	_, err = svc.teams.AddMember(ctx, user1.User.ID, team.ID, port.AddMemberInput{Email: "orphan-member@test.com"})
	require.NoError(t, err)
	task, err := svc.tasks.Create(ctx, user1.User.ID, port.CreateTaskInput{
		Title: "Assigned", TeamID: team.ID, AssigneeID: &user2.User.ID,
	})
	require.NoError(t, err)

	// Initially no orphaned assignees (assignee is a member)
	orphaned, err := svc.tasks.ListOrphaned(ctx, user1.User.ID, team.ID)
	require.NoError(t, err)
	assert.Empty(t, orphaned)

	// The member leaves; the assignment stays and is reported
	require.NoError(t, svc.teams.RemoveMember(ctx, user2.User.ID, team.ID, user2.User.ID))
	orphaned, err = svc.tasks.ListOrphaned(ctx, user1.User.ID, team.ID)
	require.NoError(t, err)
	assert.Equal(t, []domain.OrphanedAssignment{{
		TaskID: task.ID, TaskTitle: "Assigned", AssigneeID: user2.User.ID, AssigneeName: "Leaving Member",
	}}, orphaned)

	// The former member no longer sees the team's tasks
	page, err := svc.tasks.List(ctx, domain.TaskQuery{ViewerID: user2.User.ID})
	require.NoError(t, err)
	assert.Empty(t, page.Tasks)
}
