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

func TestTaskCaching_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()

	userRepo := mysqlrepo.NewUserRepo(testDB)
	teamRepo := mysqlrepo.NewTeamRepo(testDB)
	taskRepo := mysqlrepo.NewTaskRepo(testDB)
	historyRepo := mysqlrepo.NewTaskHistoryRepo(testDB)
	txManager := mysqlrepo.NewTransactionManager(testDB)
	taskCache := redis.NewTaskCache(testRedis)
	notifSvc := service.NewNotificationService()

	authSvc := service.NewAuthService(userRepo, "test-secret", 24*time.Hour)
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)

	// Setup
	user, err := authSvc.Register(ctx, domain.RegisterRequest{
		Email: "cache-test@test.com", Password: "password", FullName: "Cache User",
	})
	require.NoError(t, err)

	team, err := teamSvc.Create(ctx, user.User.ID, domain.CreateTeamRequest{
		Name: "Cache Team",
	})
	require.NoError(t, err)

	// Create tasks
	for i := 0; i < 5; i++ {
		_, err := taskSvc.Create(ctx, user.User.ID, domain.CreateTaskRequest{
			Title:  "Task " + string(rune('A'+i)),
			TeamID: team.ID,
		})
		require.NoError(t, err)
	}

	// First list - should hit DB and cache result
	filter := domain.TaskFilter{TeamID: team.ID, Page: 1, PageSize: 20}
	result1, err := taskSvc.List(ctx, user.User.ID, filter)
	require.NoError(t, err)
	assert.Equal(t, 5, result1.Total)

	// Second list - should hit cache
	result2, err := taskSvc.List(ctx, user.User.ID, filter)
	require.NoError(t, err)
	assert.Equal(t, 5, result2.Total)
	assert.Equal(t, result1.Total, result2.Total)

	// Check that data is in Redis
	cached, err := taskCache.GetTaskList(ctx, filter)
	require.NoError(t, err)
	assert.NotNil(t, cached)
	assert.Equal(t, 5, cached.Total)
}

func TestTaskPagination_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()

	userRepo := mysqlrepo.NewUserRepo(testDB)
	teamRepo := mysqlrepo.NewTeamRepo(testDB)
	taskRepo := mysqlrepo.NewTaskRepo(testDB)
	historyRepo := mysqlrepo.NewTaskHistoryRepo(testDB)
	txManager := mysqlrepo.NewTransactionManager(testDB)
	taskCache := redis.NewTaskCache(testRedis)
	notifSvc := service.NewNotificationService()

	authSvc := service.NewAuthService(userRepo, "test-secret", 24*time.Hour)
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)

	user, err := authSvc.Register(ctx, domain.RegisterRequest{
		Email: "paging@test.com", Password: "password", FullName: "Paging User",
	})
	require.NoError(t, err)

	team, err := teamSvc.Create(ctx, user.User.ID, domain.CreateTeamRequest{
		Name: "Paging Team",
	})
	require.NoError(t, err)

	for i := 0; i < 15; i++ {
		_, err := taskSvc.Create(ctx, user.User.ID, domain.CreateTaskRequest{
			Title:  "Paginated Task",
			TeamID: team.ID,
		})
		require.NoError(t, err)
	}

	// Page 1
	result, err := taskSvc.List(ctx, user.User.ID, domain.TaskFilter{
		TeamID: team.ID, Page: 1, PageSize: 5,
	})
	require.NoError(t, err)
	assert.Len(t, result.Tasks, 5)
	assert.Equal(t, 15, result.Total)
	assert.Equal(t, 3, result.TotalPages)

	// Page 2
	result2, err := taskSvc.List(ctx, user.User.ID, domain.TaskFilter{
		TeamID: team.ID, Page: 2, PageSize: 5,
	})
	require.NoError(t, err)
	assert.Len(t, result2.Tasks, 5)
}

func TestOrphanedAssignees_Integration(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()

	userRepo := mysqlrepo.NewUserRepo(testDB)
	teamRepo := mysqlrepo.NewTeamRepo(testDB)
	taskRepo := mysqlrepo.NewTaskRepo(testDB)
	historyRepo := mysqlrepo.NewTaskHistoryRepo(testDB)
	txManager := mysqlrepo.NewTransactionManager(testDB)
	taskCache := redis.NewTaskCache(testRedis)
	notifSvc := service.NewNotificationService()

	authSvc := service.NewAuthService(userRepo, "test-secret", 24*time.Hour)
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)

	user1, err := authSvc.Register(ctx, domain.RegisterRequest{
		Email: "orphan-owner@test.com", Password: "password", FullName: "Owner",
	})
	require.NoError(t, err)

	_, err = teamSvc.Create(ctx, user1.User.ID, domain.CreateTeamRequest{
		Name: "Orphan Team",
	})
	require.NoError(t, err)

	// Initially no orphaned assignees (assignee is a member)
	orphaned, err := taskSvc.GetOrphanedAssignees(ctx)
	require.NoError(t, err)
	assert.Empty(t, orphaned)
}
