package mocks

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/stretchr/testify/mock"
)

// UserRepositoryMock
type UserRepositoryMock struct {
	mock.Mock
}

func (m *UserRepositoryMock) Create(ctx context.Context, user *domain.User) (int64, error) {
	args := m.Called(ctx, user)
	return args.Get(0).(int64), args.Error(1)
}

func (m *UserRepositoryMock) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *UserRepositoryMock) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// TeamRepositoryMock
type TeamRepositoryMock struct {
	mock.Mock
}

func (m *TeamRepositoryMock) Create(ctx context.Context, team *domain.Team) (int64, error) {
	args := m.Called(ctx, team)
	return args.Get(0).(int64), args.Error(1)
}

func (m *TeamRepositoryMock) GetByID(ctx context.Context, id int64) (*domain.Team, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Team), args.Error(1)
}

func (m *TeamRepositoryMock) ListByUserID(ctx context.Context, userID int64) ([]domain.Team, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]domain.Team), args.Error(1)
}

func (m *TeamRepositoryMock) AddMember(ctx context.Context, member *domain.TeamMember) error {
	args := m.Called(ctx, member)
	return args.Error(0)
}

func (m *TeamRepositoryMock) GetMember(ctx context.Context, teamID, userID int64) (*domain.TeamMember, error) {
	args := m.Called(ctx, teamID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TeamMember), args.Error(1)
}

func (m *TeamRepositoryMock) GetStats(ctx context.Context, userID int64) ([]domain.TeamStats, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]domain.TeamStats), args.Error(1)
}

func (m *TeamRepositoryMock) GetTopContributors(ctx context.Context, teamID int64) ([]domain.TopContributor, error) {
	args := m.Called(ctx, teamID)
	return args.Get(0).([]domain.TopContributor), args.Error(1)
}

// TaskRepositoryMock
type TaskRepositoryMock struct {
	mock.Mock
}

func (m *TaskRepositoryMock) Create(ctx context.Context, task *domain.Task) (int64, error) {
	args := m.Called(ctx, task)
	return args.Get(0).(int64), args.Error(1)
}

func (m *TaskRepositoryMock) GetByID(ctx context.Context, id int64) (*domain.Task, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Task), args.Error(1)
}

func (m *TaskRepositoryMock) Update(ctx context.Context, task *domain.Task) error {
	args := m.Called(ctx, task)
	return args.Error(0)
}

func (m *TaskRepositoryMock) List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.Task), args.Int(1), args.Error(2)
}

func (m *TaskRepositoryMock) GetOrphanedAssignees(ctx context.Context) ([]domain.OrphanedAssignee, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.OrphanedAssignee), args.Error(1)
}

// TaskHistoryRepositoryMock
type TaskHistoryRepositoryMock struct {
	mock.Mock
}

func (m *TaskHistoryRepositoryMock) Create(ctx context.Context, history *domain.TaskHistory) error {
	args := m.Called(ctx, history)
	return args.Error(0)
}

func (m *TaskHistoryRepositoryMock) ListByTaskID(ctx context.Context, taskID int64) ([]domain.TaskHistory, error) {
	args := m.Called(ctx, taskID)
	return args.Get(0).([]domain.TaskHistory), args.Error(1)
}

// CommentRepositoryMock
type CommentRepositoryMock struct {
	mock.Mock
}

func (m *CommentRepositoryMock) Create(ctx context.Context, comment *domain.TaskComment) (int64, error) {
	args := m.Called(ctx, comment)
	return args.Get(0).(int64), args.Error(1)
}

func (m *CommentRepositoryMock) ListByTaskID(ctx context.Context, taskID int64) ([]domain.TaskComment, error) {
	args := m.Called(ctx, taskID)
	return args.Get(0).([]domain.TaskComment), args.Error(1)
}

// TransactionManagerMock
type TransactionManagerMock struct {
	mock.Mock
}

func (m *TransactionManagerMock) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	args := m.Called(ctx, fn)
	// Execute the function directly (no real transaction)
	if err := fn(ctx); err != nil {
		return err
	}
	return args.Error(0)
}
