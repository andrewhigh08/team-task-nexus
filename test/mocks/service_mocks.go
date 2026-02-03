package mocks

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/stretchr/testify/mock"
)

// NotificationServiceMock
type NotificationServiceMock struct {
	mock.Mock
}

func (m *NotificationServiceMock) NotifyTaskAssigned(ctx context.Context, task *domain.Task, assignee *domain.User) error {
	args := m.Called(ctx, task, assignee)
	return args.Error(0)
}

func (m *NotificationServiceMock) NotifyCommentAdded(ctx context.Context, comment *domain.TaskComment, task *domain.Task) error {
	args := m.Called(ctx, comment, task)
	return args.Error(0)
}

// TaskCacheMock
type TaskCacheMock struct {
	mock.Mock
}

func (m *TaskCacheMock) GetTaskList(ctx context.Context, filter domain.TaskFilter) (*domain.TaskListResponse, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TaskListResponse), args.Error(1)
}

func (m *TaskCacheMock) SetTaskList(ctx context.Context, filter domain.TaskFilter, response *domain.TaskListResponse) error {
	args := m.Called(ctx, filter, response)
	return args.Error(0)
}

func (m *TaskCacheMock) InvalidateTeam(ctx context.Context, teamID int64) error {
	args := m.Called(ctx, teamID)
	return args.Error(0)
}

// RateLimiterMock
type RateLimiterMock struct {
	mock.Mock
}

func (m *RateLimiterMock) Allow(ctx context.Context, userID int64) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}
