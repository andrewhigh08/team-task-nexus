package service

import (
	"context"
	"testing"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCommentService_Create_Success(t *testing.T) {
	commentRepo := new(mocks.CommentRepositoryMock)
	taskRepo := new(mocks.TaskRepositoryMock)
	teamRepo := new(mocks.TeamRepositoryMock)
	notifSvc := new(mocks.NotificationServiceMock)
	svc := NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, TeamID: 1, Title: "Test Task",
	}, nil)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleMember,
	}, nil)
	commentRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.TaskComment")).Return(int64(1), nil)
	notifSvc.On("NotifyCommentAdded", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := svc.Create(context.Background(), 1, 1, domain.CreateCommentRequest{
		Content: "This is a comment",
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(1), result.ID)
	commentRepo.AssertExpectations(t)
	notifSvc.AssertExpectations(t)
}

func TestCommentService_Create_EmptyContent(t *testing.T) {
	commentRepo := new(mocks.CommentRepositoryMock)
	taskRepo := new(mocks.TaskRepositoryMock)
	teamRepo := new(mocks.TeamRepositoryMock)
	notifSvc := new(mocks.NotificationServiceMock)
	svc := NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	result, err := svc.Create(context.Background(), 1, 1, domain.CreateCommentRequest{
		Content: "",
	})

	assert.Nil(t, result)
	assert.Error(t, err)
	appErr, ok := apperror.IsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, 400, appErr.Code)
}

func TestCommentService_Create_NotTeamMember(t *testing.T) {
	commentRepo := new(mocks.CommentRepositoryMock)
	taskRepo := new(mocks.TaskRepositoryMock)
	teamRepo := new(mocks.TeamRepositoryMock)
	notifSvc := new(mocks.NotificationServiceMock)
	svc := NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, TeamID: 1,
	}, nil)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(99)).Return(nil, nil)

	result, err := svc.Create(context.Background(), 99, 1, domain.CreateCommentRequest{
		Content: "test comment",
	})

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestCommentService_ListByTaskID_Success(t *testing.T) {
	commentRepo := new(mocks.CommentRepositoryMock)
	taskRepo := new(mocks.TaskRepositoryMock)
	teamRepo := new(mocks.TeamRepositoryMock)
	notifSvc := new(mocks.NotificationServiceMock)
	svc := NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, TeamID: 1,
	}, nil)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleMember,
	}, nil)
	commentRepo.On("ListByTaskID", mock.Anything, int64(1)).Return([]domain.TaskComment{
		{ID: 1, TaskID: 1, Content: "Comment 1"},
		{ID: 2, TaskID: 1, Content: "Comment 2"},
	}, nil)

	result, err := svc.ListByTaskID(context.Background(), 1, 1)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestCommentService_ListByTaskID_TaskNotFound(t *testing.T) {
	commentRepo := new(mocks.CommentRepositoryMock)
	taskRepo := new(mocks.TaskRepositoryMock)
	teamRepo := new(mocks.TeamRepositoryMock)
	notifSvc := new(mocks.NotificationServiceMock)
	svc := NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(999)).Return(nil, apperror.NotFound("task not found"))

	result, err := svc.ListByTaskID(context.Background(), 1, 999)

	assert.Nil(t, result)
	assert.Error(t, err)
}
