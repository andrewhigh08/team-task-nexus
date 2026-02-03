package service

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type TeamServiceImpl struct {
	teamRepo    port.TeamRepository
	userRepo    port.UserRepository
	txManager   port.TransactionManager
	notifSvc    port.NotificationService
}

func NewTeamService(
	teamRepo port.TeamRepository,
	userRepo port.UserRepository,
	txManager port.TransactionManager,
	notifSvc port.NotificationService,
) *TeamServiceImpl {
	return &TeamServiceImpl{
		teamRepo:  teamRepo,
		userRepo:  userRepo,
		txManager: txManager,
		notifSvc:  notifSvc,
	}
}

func (s *TeamServiceImpl) Create(ctx context.Context, userID int64, req domain.CreateTeamRequest) (*domain.Team, error) {
	if req.Name == "" {
		return nil, apperror.BadRequest("team name is required")
	}

	var team *domain.Team
	err := s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		t := &domain.Team{
			Name:        req.Name,
			Description: req.Description,
			OwnerID:     userID,
		}
		id, err := s.teamRepo.Create(ctx, t)
		if err != nil {
			return err
		}
		t.ID = id

		err = s.teamRepo.AddMember(ctx, &domain.TeamMember{
			TeamID: id,
			UserID: userID,
			Role:   domain.TeamRoleOwner,
		})
		if err != nil {
			return err
		}

		team, err = s.teamRepo.GetByID(ctx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return team, nil
}

func (s *TeamServiceImpl) GetByID(ctx context.Context, userID, teamID int64) (*domain.Team, error) {
	member, err := s.teamRepo.GetMember(ctx, teamID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, apperror.ErrNotTeamMember
	}
	return s.teamRepo.GetByID(ctx, teamID)
}

func (s *TeamServiceImpl) ListByUserID(ctx context.Context, userID int64) ([]domain.Team, error) {
	return s.teamRepo.ListByUserID(ctx, userID)
}

func (s *TeamServiceImpl) InviteUser(ctx context.Context, inviterID, teamID int64, req domain.InviteRequest) error {
	if req.Email == "" {
		return apperror.BadRequest("email is required")
	}

	member, err := s.teamRepo.GetMember(ctx, teamID, inviterID)
	if err != nil {
		return err
	}
	if member == nil {
		return apperror.ErrNotTeamMember
	}
	if member.Role != domain.TeamRoleOwner && member.Role != domain.TeamRoleAdmin {
		return apperror.ErrInsufficientRole
	}

	invitee, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return apperror.NotFound("user with this email not found")
	}

	role := domain.TeamRoleMember
	if req.Role == string(domain.TeamRoleAdmin) {
		role = domain.TeamRoleAdmin
	}

	return s.teamRepo.AddMember(ctx, &domain.TeamMember{
		TeamID: teamID,
		UserID: invitee.ID,
		Role:   role,
	})
}

func (s *TeamServiceImpl) GetStats(ctx context.Context, userID int64) ([]domain.TeamStats, error) {
	return s.teamRepo.GetStats(ctx, userID)
}

func (s *TeamServiceImpl) GetTopContributors(ctx context.Context, userID, teamID int64) ([]domain.TopContributor, error) {
	member, err := s.teamRepo.GetMember(ctx, teamID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, apperror.ErrNotTeamMember
	}
	return s.teamRepo.GetTopContributors(ctx, teamID)
}
