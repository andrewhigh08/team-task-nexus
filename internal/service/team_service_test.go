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

func newTeamServiceDeps() (*mocks.TeamRepositoryMock, *mocks.UserRepositoryMock, *mocks.TransactionManagerMock, *mocks.NotificationServiceMock) {
	return new(mocks.TeamRepositoryMock), new(mocks.UserRepositoryMock), new(mocks.TransactionManagerMock), new(mocks.NotificationServiceMock)
}

func TestTeamService_Create_Success(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)
	teamRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.Team")).Return(int64(1), nil)
	teamRepo.On("AddMember", mock.Anything, mock.AnythingOfType("*domain.TeamMember")).Return(nil)
	teamRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Team{
		ID: 1, Name: "Test Team", OwnerID: 1,
	}, nil)

	result, err := svc.Create(context.Background(), 1, domain.CreateTeamRequest{
		Name:        "Test Team",
		Description: "A test team",
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Test Team", result.Name)
	txManager.AssertExpectations(t)
}

func TestTeamService_Create_EmptyName(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	result, err := svc.Create(context.Background(), 1, domain.CreateTeamRequest{Name: ""})

	assert.Nil(t, result)
	assert.Error(t, err)
	appErr, ok := apperror.IsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, 400, appErr.Code)
}

func TestTeamService_GetByID_Success(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	teamRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Team{
		ID: 1, Name: "Test Team",
	}, nil)

	result, err := svc.GetByID(context.Background(), 1, 1)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Test Team", result.Name)
}

func TestTeamService_GetByID_NotMember(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(2)).Return(nil, nil)

	result, err := svc.GetByID(context.Background(), 2, 1)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestTeamService_InviteUser_Success(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	userRepo.On("GetByEmail", mock.Anything, "newuser@example.com").Return(&domain.User{
		ID: 2, Email: "newuser@example.com",
	}, nil)
	teamRepo.On("AddMember", mock.Anything, mock.AnythingOfType("*domain.TeamMember")).Return(nil)

	err := svc.InviteUser(context.Background(), 1, 1, domain.InviteRequest{
		Email: "newuser@example.com",
		Role:  "member",
	})

	assert.NoError(t, err)
	teamRepo.AssertExpectations(t)
	userRepo.AssertExpectations(t)
}

func TestTeamService_InviteUser_InsufficientRole(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(2)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 2, Role: domain.TeamRoleMember,
	}, nil)

	err := svc.InviteUser(context.Background(), 2, 1, domain.InviteRequest{
		Email: "newuser@example.com",
	})

	assert.Error(t, err)
	assert.Equal(t, apperror.ErrInsufficientRole, err)
}

func TestTeamService_ListByUserID(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	expected := []domain.Team{
		{ID: 1, Name: "Team 1"},
		{ID: 2, Name: "Team 2"},
	}
	teamRepo.On("ListByUserID", mock.Anything, int64(1)).Return(expected, nil)

	result, err := svc.ListByUserID(context.Background(), 1)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestTeamService_GetStats_Success(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	expected := []domain.TeamStats{
		{ID: 1, Name: "Team 1", MemberCount: 5, DoneLast7D: 3},
	}
	teamRepo.On("GetStats", mock.Anything, int64(1)).Return(expected, nil)

	result, err := svc.GetStats(context.Background(), 1)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, 5, result[0].MemberCount)
}

func TestTeamService_InviteUser_EmptyEmail(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	err := svc.InviteUser(context.Background(), 1, 1, domain.InviteRequest{Email: ""})

	assert.Error(t, err)
	appErr, ok := apperror.IsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, 400, appErr.Code)
}

func TestTeamService_InviteUser_NotMember(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(99)).Return(nil, nil)

	err := svc.InviteUser(context.Background(), 99, 1, domain.InviteRequest{Email: "user@example.com"})

	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestTeamService_InviteUser_AsAdmin(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleAdmin,
	}, nil)
	userRepo.On("GetByEmail", mock.Anything, "newuser@example.com").Return(&domain.User{
		ID: 3, Email: "newuser@example.com",
	}, nil)
	teamRepo.On("AddMember", mock.Anything, mock.AnythingOfType("*domain.TeamMember")).Return(nil)

	err := svc.InviteUser(context.Background(), 1, 1, domain.InviteRequest{
		Email: "newuser@example.com",
		Role:  "admin",
	})

	assert.NoError(t, err)
}

func TestTeamService_GetTopContributors_Success(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleMember,
	}, nil)
	expected := []domain.TopContributor{
		{UserID: 1, FullName: "User 1", TasksCreated: 10, Rank: 1},
	}
	teamRepo.On("GetTopContributors", mock.Anything, int64(1)).Return(expected, nil)

	result, err := svc.GetTopContributors(context.Background(), 1, 1)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, 10, result[0].TasksCreated)
}

func TestTeamService_GetTopContributors_NotMember(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(99)).Return(nil, nil)

	result, err := svc.GetTopContributors(context.Background(), 99, 1)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestTeamService_InviteUser_UserNotFound(t *testing.T) {
	teamRepo, userRepo, txManager, notifSvc := newTeamServiceDeps()
	svc := NewTeamService(teamRepo, userRepo, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	userRepo.On("GetByEmail", mock.Anything, "unknown@example.com").Return(nil, apperror.NotFound("user not found"))

	err := svc.InviteUser(context.Background(), 1, 1, domain.InviteRequest{
		Email: "unknown@example.com",
	})

	assert.Error(t, err)
}
