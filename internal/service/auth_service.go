package service

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type AuthServiceImpl struct {
	userRepo  port.UserRepository
	jwtSecret string
	jwtExpiry time.Duration
}

func NewAuthService(userRepo port.UserRepository, jwtSecret string, jwtExpiry time.Duration) *AuthServiceImpl {
	return &AuthServiceImpl{
		userRepo:  userRepo,
		jwtSecret: jwtSecret,
		jwtExpiry: jwtExpiry,
	}
}

func (s *AuthServiceImpl) Register(ctx context.Context, req domain.RegisterRequest) (*domain.AuthResponse, error) {
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		return nil, apperror.BadRequest("email, password, and full_name are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperror.Internal("hash password", err)
	}

	user := &domain.User{
		Email:        req.Email,
		PasswordHash: string(hash),
		FullName:     req.FullName,
	}

	id, err := s.userRepo.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	user.ID = id
	token, err := s.generateToken(user.ID)
	if err != nil {
		return nil, apperror.Internal("generate token", err)
	}

	return &domain.AuthResponse{Token: token, User: *user}, nil
}

func (s *AuthServiceImpl) Login(ctx context.Context, req domain.LoginRequest) (*domain.AuthResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, apperror.BadRequest("email and password are required")
	}

	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, apperror.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, apperror.ErrInvalidCredentials
	}

	token, err := s.generateToken(user.ID)
	if err != nil {
		return nil, apperror.Internal("generate token", err)
	}

	return &domain.AuthResponse{Token: token, User: *user}, nil
}

func (s *AuthServiceImpl) generateToken(userID int64) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(s.jwtExpiry).Unix(),
		"iat":     time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}
