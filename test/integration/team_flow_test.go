//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/shalfey088/team-task-nexus/internal/adapter/cache/redis"
	mysqlrepo "github.com/shalfey088/team-task-nexus/internal/adapter/repository/mysql"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamFlow_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()

	userRepo := mysqlrepo.NewUserRepo(testDB)
	teamRepo := mysqlrepo.NewTeamRepo(testDB)
	taskRepo := mysqlrepo.NewTaskRepo(testDB)
	historyRepo := mysqlrepo.NewTaskHistoryRepo(testDB)
	commentRepo := mysqlrepo.NewCommentRepo(testDB)
	txManager := mysqlrepo.NewTransactionManager(testDB)
	taskCache := redis.NewTaskCache(testRedis)
	notifSvc := service.NewNotificationService()

	authSvc := service.NewAuthService(userRepo, "test-secret", 24*time.Hour)
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)
	commentSvc := service.NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	// Register two users
	user1, err := authSvc.Register(ctx, domain.RegisterRequest{
		Email: "owner@test.com", Password: "password", FullName: "Owner User",
	})
	require.NoError(t, err)
	require.NotEmpty(t, user1.Token)

	user2, err := authSvc.Register(ctx, domain.RegisterRequest{
		Email: "member@test.com", Password: "password", FullName: "Member User",
	})
	require.NoError(t, err)

	// Create team
	team, err := teamSvc.Create(ctx, user1.User.ID, domain.CreateTeamRequest{
		Name: "Test Team", Description: "Integration test team",
	})
	require.NoError(t, err)
	assert.Equal(t, "Test Team", team.Name)

	// Invite member
	err = teamSvc.InviteUser(ctx, user1.User.ID, team.ID, domain.InviteRequest{
		Email: "member@test.com", Role: "member",
	})
	require.NoError(t, err)

	// List teams for user2
	teams, err := teamSvc.ListByUserID(ctx, user2.User.ID)
	require.NoError(t, err)
	assert.Len(t, teams, 1)

	// Create task
	task, err := taskSvc.Create(ctx, user1.User.ID, domain.CreateTaskRequest{
		Title: "Integration Test Task", TeamID: team.ID, Priority: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusTodo, task.Status)

	// Update task
	newStatus := "in_progress"
	updated, err := taskSvc.Update(ctx, user1.User.ID, task.ID, domain.UpdateTaskRequest{
		Status: &newStatus,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusInProgress, updated.Status)

	// Check history
	history, err := taskSvc.GetHistory(ctx, user1.User.ID, task.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(history), 1)

	// Add comment
	comment, err := commentSvc.Create(ctx, user2.User.ID, task.ID, domain.CreateCommentRequest{
		Content: "Working on this task!",
	})
	require.NoError(t, err)
	assert.Equal(t, "Working on this task!", comment.Content)

	// List comments
	comments, err := commentSvc.ListByTaskID(ctx, user2.User.ID, task.ID)
	require.NoError(t, err)
	assert.Len(t, comments, 1)

	// Get team stats
	stats, err := teamSvc.GetStats(ctx, user1.User.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(stats), 1)

	// Get top contributors
	contributors, err := teamSvc.GetTopContributors(ctx, user1.User.ID, team.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(contributors), 1)
}
