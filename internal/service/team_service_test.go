package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

func (f *fixture) teamDeps() TeamDeps {
	return TeamDeps{Teams: f.teams, Users: f.users, Tx: f.tx, Clock: fixedClock}
}

func (f *fixture) teamService(t *testing.T) *TeamService {
	t.Helper()
	svc, err := NewTeamService(f.teamDeps())
	require.NoError(t, err)
	return svc
}

func testTeam() *domain.Team {
	return &domain.Team{
		ID:          testTeamID,
		Name:        "Platform",
		Description: "Keeps the lights on",
		OwnerID:     ownerUser,
		CreatedAt:   testNow.Add(-time.Hour),
		UpdatedAt:   testNow.Add(-time.Hour),
	}
}

func TestNewTeamService_RejectsMissingDeps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*TeamDeps)
		want   string
	}{
		{name: "team repository", mutate: func(d *TeamDeps) { d.Teams = nil }, want: "team repository is required"},
		{name: "user repository", mutate: func(d *TeamDeps) { d.Users = nil }, want: "user repository is required"},
		{name: "transaction manager", mutate: func(d *TeamDeps) { d.Tx = nil }, want: "transaction manager is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := newFixture(t).teamDeps()
			tt.mutate(&deps)

			svc, err := NewTeamService(deps)

			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, svc)
		})
	}
}

func TestTeamService_Create(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created := testTeam()
	f.expectTx()
	gomock.InOrder(
		f.teams.EXPECT().Create(inTx, &domain.Team{Name: "Platform", Description: "Keeps the lights on", OwnerID: ownerUser}).
			Return(testTeamID, nil),
		f.teams.EXPECT().AddMember(inTx, domain.TeamMember{TeamID: testTeamID, UserID: ownerUser, Role: domain.TeamRoleOwner}).
			Return(nil),
		f.teams.EXPECT().GetByID(inTx, testTeamID).Return(created, nil),
	)

	team, err := f.teamService(t).Create(t.Context(), ownerUser, port.CreateTeamInput{
		Name:        "  Platform ",
		Description: " Keeps the lights on\n",
	})

	require.NoError(t, err)
	assert.Same(t, created, team)
}

func TestTeamService_Create_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   port.CreateTeamInput
		want map[string]string
	}{
		{name: "blank name", in: port.CreateTeamInput{Name: " \t "}, want: map[string]string{"name": "is required"}},
		{
			name: "name too long", in: port.CreateTeamInput{Name: strings.Repeat("ж", maxTeamNameChars+1)},
			want: map[string]string{"name": "must be at most 255 characters"},
		},
		{
			name: "description too long",
			in:   port.CreateTeamInput{Name: "Platform", Description: strings.Repeat("x", maxTeamDescriptionChars+1)},
			want: map[string]string{"description": "must be at most 5000 characters"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// No repository call is expected: invalid input never reaches the database.
			team, err := newFixture(t).teamService(t).Create(t.Context(), ownerUser, tt.in)

			requireFieldErrors(t, err, tt.want)
			assert.Nil(t, team)
		})
	}
}

func TestTeamService_Create_FailsWhenTheOwnerCannotBeAdded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	boom := apperror.Internal("add team member", errors.New("deadlock"))
	result := f.expectTx()
	f.teams.EXPECT().Create(inTx, gomock.Any()).Return(testTeamID, nil)
	f.teams.EXPECT().AddMember(inTx, gomock.Any()).Return(boom)

	team, err := f.teamService(t).Create(t.Context(), ownerUser, port.CreateTeamInput{Name: "Platform"})

	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, result.err, boom, "the transaction must be rolled back")
	assert.Nil(t, team)
}

func TestTeamService_Get(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
		f.teams.EXPECT().GetByID(gomock.Any(), testTeamID).Return(testTeam(), nil)

		team, err := f.teamService(t).Get(t.Context(), memberUser, testTeamID)

		require.NoError(t, err)
		assert.Equal(t, testTeam(), team)
	})

	t.Run("outsider gets the not found of a missing team", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectRole(gomock.Any(), outsiderUser, "")

		team, err := f.teamService(t).Get(t.Context(), outsiderUser, testTeamID)

		requireNotFound(t, err, "team", testTeamID)
		assert.Nil(t, team)
	})

	t.Run("failing membership lookup is not a not found", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		boom := apperror.Internal("get team member", errors.New("connection reset"))
		f.teams.EXPECT().GetMember(gomock.Any(), testTeamID, memberUser).Return(nil, boom)

		_, err := f.teamService(t).Get(t.Context(), memberUser, testTeamID)

		require.ErrorIs(t, err, boom)
	})
}

func TestTeamService_List(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	want := []domain.TeamSummary{{Team: *testTeam(), Role: domain.TeamRoleAdmin}}
	f.teams.EXPECT().ListForUser(gomock.Any(), adminUser).Return(want, nil)

	teams, err := f.teamService(t).List(t.Context(), adminUser)

	require.NoError(t, err)
	assert.Equal(t, want, teams)
}

func TestTeamService_ListMembers(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		want := []domain.MemberProfile{{UserID: ownerUser, Email: "owner@example.com", FullName: "Olga Owner", Role: domain.TeamRoleOwner}}
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
		f.teams.EXPECT().ListMembers(gomock.Any(), testTeamID).Return(want, nil)

		members, err := f.teamService(t).ListMembers(t.Context(), memberUser, testTeamID)

		require.NoError(t, err)
		assert.Equal(t, want, members)
	})

	t.Run("outsider", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectRole(gomock.Any(), outsiderUser, "")

		_, err := f.teamService(t).ListMembers(t.Context(), outsiderUser, testTeamID)

		requireNotFound(t, err, "team", testTeamID)
	})
}

func TestTeamService_AddMember(t *testing.T) {
	t.Parallel()
	const newUser int64 = 9
	invitee := &domain.User{ID: newUser, Email: "nina@example.com", FullName: "Nina New"}
	tests := []struct {
		name       string
		caller     int64
		callerRole domain.TeamRole
		role       domain.TeamRole
		wantRole   domain.TeamRole
	}{
		{name: "owner adds a member by default", caller: ownerUser, callerRole: domain.TeamRoleOwner, wantRole: domain.TeamRoleMember},
		{name: "owner adds an admin", caller: ownerUser, callerRole: domain.TeamRoleOwner, role: domain.TeamRoleAdmin, wantRole: domain.TeamRoleAdmin},
		{name: "admin adds a member", caller: adminUser, callerRole: domain.TeamRoleAdmin, role: domain.TeamRoleMember, wantRole: domain.TeamRoleMember},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.expectRole(gomock.Any(), tt.caller, tt.callerRole)
			f.users.EXPECT().GetByEmail(gomock.Any(), "nina@example.com").Return(invitee, nil)
			f.teams.EXPECT().AddMember(gomock.Any(), domain.TeamMember{TeamID: testTeamID, UserID: newUser, Role: tt.wantRole}).Return(nil)

			member, err := f.teamService(t).AddMember(t.Context(), tt.caller, testTeamID, port.AddMemberInput{
				Email: " Nina@Example.com ",
				Role:  tt.role,
			})

			require.NoError(t, err)
			assert.Equal(t, &domain.MemberProfile{UserID: newUser, Email: "nina@example.com", FullName: "Nina New", Role: tt.wantRole}, member)
		})
	}
}

func TestTeamService_AddMember_Rejections(t *testing.T) {
	t.Parallel()
	valid := port.AddMemberInput{Email: "nina@example.com", Role: domain.TeamRoleMember}
	tests := []struct {
		name   string
		caller int64
		in     port.AddMemberInput
		setup  func(f *fixture)
		check  func(t *testing.T, err error)
	}{
		{
			name:   "owner role cannot be given",
			caller: ownerUser,
			in:     port.AddMemberInput{Email: "nina@example.com", Role: domain.TeamRoleOwner},
			check: func(t *testing.T, err error) {
				requireFieldErrors(t, err, map[string]string{"role": "must be admin or member"})
			},
		},
		{
			name:   "unknown role and invalid email",
			caller: ownerUser,
			in:     port.AddMemberInput{Email: "nina", Role: "guest"},
			check: func(t *testing.T, err error) {
				requireFieldErrors(t, err, map[string]string{"email": "must be a valid email address", "role": "must be admin or member"})
			},
		},
		{
			name:   "outsider",
			caller: outsiderUser,
			in:     valid,
			setup:  func(f *fixture) { f.expectRole(gomock.Any(), outsiderUser, "") },
			check:  func(t *testing.T, err error) { requireNotFound(t, err, "team", testTeamID) },
		},
		{
			name:   "plain member",
			caller: memberUser,
			in:     valid,
			setup:  func(f *fixture) { f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember) },
			check:  func(t *testing.T, err error) { assertAppError(t, err, apperror.CodeForbidden) },
		},
		{
			name:   "admin adding an admin",
			caller: adminUser,
			in:     port.AddMemberInput{Email: "nina@example.com", Role: domain.TeamRoleAdmin},
			setup:  func(f *fixture) { f.expectRole(gomock.Any(), adminUser, domain.TeamRoleAdmin) },
			check: func(t *testing.T, err error) {
				appErr := assertAppError(t, err, apperror.CodeForbidden)
				assert.Equal(t, "only the team owner may add admins", appErr.Message)
			},
		},
		{
			name:   "unknown email",
			caller: ownerUser,
			in:     valid,
			setup: func(f *fixture) {
				f.expectRole(gomock.Any(), ownerUser, domain.TeamRoleOwner)
				f.users.EXPECT().GetByEmail(gomock.Any(), "nina@example.com").Return(nil, apperror.NotFound("user", "nina@example.com"))
			},
			check: func(t *testing.T, err error) { requireNotFound(t, err, "user", "nina@example.com") },
		},
		{
			name:   "already a member",
			caller: ownerUser,
			in:     valid,
			setup: func(f *fixture) {
				f.expectRole(gomock.Any(), ownerUser, domain.TeamRoleOwner)
				f.users.EXPECT().GetByEmail(gomock.Any(), "nina@example.com").Return(&domain.User{ID: memberUser}, nil)
				f.teams.EXPECT().AddMember(gomock.Any(), gomock.Any()).Return(apperror.Conflict("team member", "user_id", memberUser))
			},
			check: func(t *testing.T, err error) { assertAppError(t, err, apperror.CodeConflict) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			member, err := f.teamService(t).AddMember(t.Context(), tt.caller, testTeamID, tt.in)

			require.Error(t, err)
			tt.check(t, err)
			assert.Nil(t, member)
		})
	}
}

func TestTeamService_RemoveMember(t *testing.T) {
	t.Parallel()
	const otherAdmin int64 = 5
	tests := []struct {
		name       string
		caller     int64
		callerRole domain.TeamRole
		target     int64
		// targetRole is looked up only when someone else is removed; empty means no lookup.
		targetRole domain.TeamRole
		wantCode   string
	}{
		{name: "owner removes an admin", caller: ownerUser, callerRole: domain.TeamRoleOwner, target: adminUser, targetRole: domain.TeamRoleAdmin},
		{name: "owner removes a member", caller: ownerUser, callerRole: domain.TeamRoleOwner, target: memberUser, targetRole: domain.TeamRoleMember},
		{name: "admin removes a member", caller: adminUser, callerRole: domain.TeamRoleAdmin, target: memberUser, targetRole: domain.TeamRoleMember},
		{name: "member leaves", caller: memberUser, callerRole: domain.TeamRoleMember, target: memberUser},
		{name: "admin leaves", caller: adminUser, callerRole: domain.TeamRoleAdmin, target: adminUser},
		{
			name: "owner cannot leave", caller: ownerUser, callerRole: domain.TeamRoleOwner, target: ownerUser,
			wantCode: apperror.CodeForbidden,
		},
		{
			name: "admin cannot remove another admin", caller: adminUser, callerRole: domain.TeamRoleAdmin, target: otherAdmin,
			targetRole: domain.TeamRoleAdmin, wantCode: apperror.CodeForbidden,
		},
		{
			name: "admin cannot remove the owner", caller: adminUser, callerRole: domain.TeamRoleAdmin, target: ownerUser,
			targetRole: domain.TeamRoleOwner, wantCode: apperror.CodeForbidden,
		},
		{
			name: "member cannot remove others", caller: memberUser, callerRole: domain.TeamRoleMember, target: adminUser,
			wantCode: apperror.CodeForbidden,
		},
		{name: "outsider", caller: outsiderUser, target: memberUser, wantCode: apperror.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.expectRole(gomock.Any(), tt.caller, tt.callerRole)
			if tt.targetRole != "" {
				f.expectRole(gomock.Any(), tt.target, tt.targetRole)
			}
			if tt.wantCode == "" {
				f.teams.EXPECT().RemoveMember(gomock.Any(), testTeamID, tt.target).Return(nil)
			}

			err := f.teamService(t).RemoveMember(t.Context(), tt.caller, testTeamID, tt.target)

			if tt.wantCode == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assertAppError(t, err, tt.wantCode)
		})
	}
}

func TestTeamService_RemoveMember_UnknownTarget(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	const stranger int64 = 99
	f.expectRole(gomock.Any(), ownerUser, domain.TeamRoleOwner)
	f.expectRole(gomock.Any(), stranger, "")

	err := f.teamService(t).RemoveMember(t.Context(), ownerUser, testTeamID, stranger)

	requireNotFound(t, err, "team member", stranger)
}

func TestTeamService_Stats(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	want := []domain.TeamStats{{TeamID: testTeamID, Name: "Platform", MemberCount: 3, OpenTasks: 4, DoneLast7Days: 2}}
	f.teams.EXPECT().Stats(gomock.Any(), memberUser, testNow.Add(-7*24*time.Hour)).Return(want, nil)

	stats, err := f.teamService(t).Stats(t.Context(), memberUser)

	require.NoError(t, err)
	assert.Equal(t, want, stats)
}

func TestTeamService_TopContributors(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		want := []domain.TopContributor{{UserID: adminUser, FullName: "Ada Admin", TasksCompleted: 5, Rank: 1}}
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
		f.teams.EXPECT().TopContributors(gomock.Any(), testTeamID, testNow.Add(-30*24*time.Hour), 3).Return(want, nil)

		contributors, err := f.teamService(t).TopContributors(t.Context(), memberUser, testTeamID)

		require.NoError(t, err)
		assert.Equal(t, want, contributors)
	})

	t.Run("outsider", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectRole(gomock.Any(), outsiderUser, "")

		_, err := f.teamService(t).TopContributors(t.Context(), outsiderUser, testTeamID)

		requireNotFound(t, err, "team", testTeamID)
	})
}
