package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// Analytics windows. Stats count the tasks completed in the last statsWindow; contributors
// are ranked by the tasks they completed in the last contributorsWindow.
const (
	statsWindow          = 7 * 24 * time.Hour
	contributorsWindow   = 30 * 24 * time.Hour
	topContributorsLimit = 3
)

// TeamDeps lists the collaborators of TeamService. A nil Clock means time.Now.
type TeamDeps struct {
	Teams port.TeamRepository
	Users port.UserRepository
	Tx    port.TxManager
	Clock func() time.Time
}

// TeamService implements port.TeamService.
type TeamService struct {
	teams port.TeamRepository
	users port.UserRepository
	tx    port.TxManager
	now   func() time.Time
}

// NewTeamService validates deps and returns a TeamService.
func NewTeamService(deps TeamDeps) (*TeamService, error) {
	var missing string
	switch {
	case deps.Teams == nil:
		missing = "team repository"
	case deps.Users == nil:
		missing = "user repository"
	case deps.Tx == nil:
		missing = "transaction manager"
	}
	if missing != "" {
		return nil, errors.New("team service: " + missing + " is required")
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &TeamService{teams: deps.Teams, users: deps.Users, tx: deps.Tx, now: deps.Clock}, nil
}

// Create creates a team and makes userID its owner, in one transaction.
func (s *TeamService) Create(ctx context.Context, userID int64, in port.CreateTeamInput) (*domain.Team, error) {
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	errs := fieldErrors{}
	errs.checkRequiredText("name", name, maxTeamNameChars)
	errs.checkOptionalText("description", description, maxTeamDescriptionChars)
	if err := errs.err(); err != nil {
		return nil, err
	}

	var team *domain.Team
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		team, err = s.createOwned(ctx, &domain.Team{Name: name, Description: description, OwnerID: userID})
		return err
	})
	if err != nil {
		return nil, err
	}
	return team, nil
}

// createOwned inserts team with its owner as the first member and returns it as stored.
func (s *TeamService) createOwned(ctx context.Context, team *domain.Team) (*domain.Team, error) {
	id, err := s.teams.Create(ctx, team)
	if err != nil {
		return nil, err
	}
	owner := domain.TeamMember{TeamID: id, UserID: team.OwnerID, Role: domain.TeamRoleOwner}
	if err := s.teams.AddMember(ctx, owner); err != nil {
		return nil, err
	}
	return s.teams.GetByID(ctx, id)
}

// Get returns the team to one of its members.
func (s *TeamService) Get(ctx context.Context, userID, teamID int64) (*domain.Team, error) {
	if _, err := requireMember(ctx, s.teams, teamID, userID); err != nil {
		return nil, err
	}
	return s.teams.GetByID(ctx, teamID)
}

// List returns the teams of userID.
func (s *TeamService) List(ctx context.Context, userID int64) ([]domain.TeamSummary, error) {
	return s.teams.ListForUser(ctx, userID)
}

// ListMembers returns the members of the team to one of its members.
func (s *TeamService) ListMembers(ctx context.Context, userID, teamID int64) ([]domain.MemberProfile, error) {
	if _, err := requireMember(ctx, s.teams, teamID, userID); err != nil {
		return nil, err
	}
	return s.teams.ListMembers(ctx, teamID)
}

// AddMember adds the registered user with in.Email to the team.
func (s *TeamService) AddMember(ctx context.Context, userID, teamID int64, in port.AddMemberInput) (*domain.MemberProfile, error) {
	email := normalizeEmail(in.Email)
	role := in.Role
	if role == "" {
		role = domain.TeamRoleMember
	}
	errs := fieldErrors{}
	errs.checkEmail("email", email)
	if role != domain.TeamRoleAdmin && role != domain.TeamRoleMember {
		errs.add("role", "must be admin or member")
	}
	if err := errs.err(); err != nil {
		return nil, err
	}

	caller, err := requireManager(ctx, s.teams, teamID, userID)
	if err != nil {
		return nil, err
	}
	if role == domain.TeamRoleAdmin && caller.Role != domain.TeamRoleOwner {
		return nil, apperror.Forbidden("only the team owner may add admins")
	}
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if err := s.teams.AddMember(ctx, domain.TeamMember{TeamID: teamID, UserID: user.ID, Role: role}); err != nil {
		return nil, err
	}
	return &domain.MemberProfile{UserID: user.ID, Email: user.Email, FullName: user.FullName, Role: role}, nil
}

// RemoveMember removes memberID from the team. The removed member's task assignments are
// kept; ListOrphaned reports them so that they can be reassigned.
func (s *TeamService) RemoveMember(ctx context.Context, userID, teamID, memberID int64) error {
	caller, err := requireMember(ctx, s.teams, teamID, userID)
	if err != nil {
		return err
	}
	if err := s.checkRemoval(ctx, caller, memberID); err != nil {
		return err
	}
	return s.teams.RemoveMember(ctx, teamID, memberID)
}

// checkRemoval decides whether caller may remove memberID from caller's team. Anyone but the
// owner may leave; the owner cannot, since ownership cannot be transferred. The owner may
// remove anyone else, an admin only plain members.
func (s *TeamService) checkRemoval(ctx context.Context, caller *domain.TeamMember, memberID int64) error {
	if memberID == caller.UserID {
		if caller.Role == domain.TeamRoleOwner {
			return apperror.Forbidden("the team owner cannot leave the team")
		}
		return nil
	}
	if !caller.Role.CanManageMembers() {
		return errManagerRequired()
	}
	target, err := s.teams.GetMember(ctx, caller.TeamID, memberID)
	if err != nil {
		return err
	}
	if caller.Role == domain.TeamRoleAdmin && target.Role != domain.TeamRoleMember {
		return apperror.Forbidden("admins may remove only members")
	}
	return nil
}

// Stats summarizes every team of userID over the last statsWindow.
func (s *TeamService) Stats(ctx context.Context, userID int64) ([]domain.TeamStats, error) {
	return s.teams.Stats(ctx, userID, s.now().Add(-statsWindow))
}

// TopContributors returns the topContributorsLimit best ranked members of the team over the
// last contributorsWindow, to one of its members.
func (s *TeamService) TopContributors(ctx context.Context, userID, teamID int64) ([]domain.TopContributor, error) {
	if _, err := requireMember(ctx, s.teams, teamID, userID); err != nil {
		return nil, err
	}
	return s.teams.TopContributors(ctx, teamID, s.now().Add(-contributorsWindow), topContributorsLimit)
}
