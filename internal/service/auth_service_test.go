package service

import (
	"context"
	"testing"
	"time"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthService_Register_Success(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	userRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.User")).Return(int64(1), nil)

	req := domain.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	result, err := svc.Register(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.Token)
	assert.Equal(t, int64(1), result.User.ID)
	assert.Equal(t, "test@example.com", result.User.Email)
	userRepo.AssertExpectations(t)
}

func TestAuthService_Register_EmptyFields(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	req := domain.RegisterRequest{Email: "", Password: "", FullName: ""}
	result, err := svc.Register(context.Background(), req)

	assert.Nil(t, result)
	assert.Error(t, err)
	appErr, ok := apperror.IsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, 400, appErr.Code)
}

func TestAuthService_Register_EmailTaken(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	userRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.User")).Return(int64(0), apperror.ErrEmailTaken)

	req := domain.RegisterRequest{
		Email:    "taken@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	result, err := svc.Register(context.Background(), req)

	assert.Nil(t, result)
	assert.Error(t, err)
	appErr, ok := apperror.IsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, 409, appErr.Code)
	userRepo.AssertExpectations(t)
}

func TestAuthService_Login_Success(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	user := &domain.User{
		ID:           1,
		Email:        "test@example.com",
		PasswordHash: string(hash),
		FullName:     "Test User",
	}
	userRepo.On("GetByEmail", mock.Anything, "test@example.com").Return(user, nil)

	req := domain.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	result, err := svc.Login(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.Token)
	assert.Equal(t, int64(1), result.User.ID)
	userRepo.AssertExpectations(t)
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	user := &domain.User{
		ID:           1,
		Email:        "test@example.com",
		PasswordHash: string(hash),
		FullName:     "Test User",
	}
	userRepo.On("GetByEmail", mock.Anything, "test@example.com").Return(user, nil)

	req := domain.LoginRequest{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	result, err := svc.Login(context.Background(), req)

	assert.Nil(t, result)
	assert.Error(t, err)
	userRepo.AssertExpectations(t)
}

func TestAuthService_Login_UserNotFound(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	userRepo.On("GetByEmail", mock.Anything, "notfound@example.com").Return(nil, apperror.NotFound("user not found"))

	req := domain.LoginRequest{
		Email:    "notfound@example.com",
		Password: "password123",
	}

	result, err := svc.Login(context.Background(), req)

	assert.Nil(t, result)
	assert.Error(t, err)
	userRepo.AssertExpectations(t)
}

func TestAuthService_Login_EmptyFields(t *testing.T) {
	userRepo := new(mocks.UserRepositoryMock)
	svc := NewAuthService(userRepo, "test-secret", 24*time.Hour)

	req := domain.LoginRequest{Email: "", Password: ""}
	result, err := svc.Login(context.Background(), req)

	assert.Nil(t, result)
	assert.Error(t, err)
	appErr, ok := apperror.IsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, 400, appErr.Code)
}
