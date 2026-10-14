package handler

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

type teamTest = handlerTest[*mocks.MockTeamService]

// mockedTeamHandler returns a TeamHandler on a mock service prepared by expect.
func mockedTeamHandler(t *testing.T, expect func(*mocks.MockTeamService)) *TeamHandler {
	svc := mocks.NewMockTeamService(gomock.NewController(t))
	if expect != nil {
		expect(svc)
	}
	return NewTeamHandler(svc)
}

func testTeam() *domain.Team {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	return &domain.Team{ID: 7, Name: "Platform", Description: "Keeps the lights on", OwnerID: 1, CreatedAt: created, UpdatedAt: created}
}

const testTeamJSON = `{"id":7,"name":"Platform","description":"Keeps the lights on","owner_id":1,` +
	`"created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-01T09:00:00Z"}`

func TestTeamHandler_Create(t *testing.T) {
	t.Parallel()
	body := `{"name":"Platform","description":"Keeps the lights on"}`
	wantInput := port.CreateTeamInput{Name: "Platform", Description: "Keeps the lights on"}
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).Create, http.MethodPost, "/teams", []teamTest{
		{
			name: "created", target: "/teams", body: body,
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Create(gomock.Any(), testUserID, wantInput).Return(testTeam(), nil)
			},
			wantStatus: http.StatusCreated, wantBody: `{"success":true,"data":` + testTeamJSON + `}`,
		},
		{name: "malformed body", target: "/teams", body: `{"name":`, wantStatus: http.StatusBadRequest, wantBody: invalidBody},
		{
			name: "invalid input", target: "/teams", body: `{"name":"","description":""}`,
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Create(gomock.Any(), testUserID, port.CreateTeamInput{}).
					Return(nil, apperror.ValidationError("invalid input", map[string]any{"fields": map[string]string{"name": "is required"}}))
			},
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("name", "is required"),
		},
		{
			name: "failure is not exposed", target: "/teams", body: body,
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Create(gomock.Any(), testUserID, wantInput).Return(nil, errors.New("deadlock found"))
			},
			wantStatus: http.StatusInternalServerError, wantBody: internalError,
		},
	})
}

func TestTeamHandler_List(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).List, http.MethodGet, "/teams", []teamTest{
		{
			name: "teams with the caller's role", target: "/teams",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().List(gomock.Any(), testUserID).Return([]domain.TeamSummary{{Team: *testTeam(), Role: domain.TeamRoleAdmin}}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody: `{"success":true,"data":[{"id":7,"name":"Platform","description":"Keeps the lights on","owner_id":1,` +
				`"created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-01T09:00:00Z","role":"admin"}]}`,
		},
		{
			name: "no teams", target: "/teams",
			expect:     func(svc *mocks.MockTeamService) { svc.EXPECT().List(gomock.Any(), testUserID).Return(nil, nil) },
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":[]}`,
		},
	})
}

func TestTeamHandler_Get(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).Get, http.MethodGet, "/teams/{id}", []teamTest{
		{
			name: "found", target: "/teams/7",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Get(gomock.Any(), testUserID, int64(7)).Return(testTeam(), nil)
			},
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":` + testTeamJSON + `}`,
		},
		{name: "malformed id", target: "/teams/x", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "team of others", target: "/teams/8",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Get(gomock.Any(), testUserID, int64(8)).Return(nil, apperror.NotFound("team", int64(8)))
			},
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("team", 8),
		},
	})
}

func TestTeamHandler_ListMembers(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).ListMembers, http.MethodGet, "/teams/{id}/members", []teamTest{
		{
			name: "members", target: "/teams/7/members",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().ListMembers(gomock.Any(), testUserID, int64(7)).Return([]domain.MemberProfile{
					{UserID: 1, Email: "olga@example.com", FullName: "Olga Owner", Role: domain.TeamRoleOwner},
					{UserID: 3, Email: "max@example.com", FullName: "Max Member", Role: domain.TeamRoleMember},
				}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody: `{"success":true,"data":[{"user_id":1,"email":"olga@example.com","full_name":"Olga Owner","role":"owner"},` +
				`{"user_id":3,"email":"max@example.com","full_name":"Max Member","role":"member"}]}`,
		},
		{name: "malformed id", target: "/teams/0/members", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "team of others", target: "/teams/7/members",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().ListMembers(gomock.Any(), testUserID, int64(7)).Return(nil, apperror.NotFound("team", int64(7)))
			},
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("team", 7),
		},
	})
}

func TestTeamHandler_AddMember(t *testing.T) {
	t.Parallel()
	body := `{"email":"nina@example.com","role":"admin"}`
	wantInput := port.AddMemberInput{Email: "nina@example.com", Role: domain.TeamRoleAdmin}
	failing := func(err error) func(*mocks.MockTeamService) {
		return func(svc *mocks.MockTeamService) {
			svc.EXPECT().AddMember(gomock.Any(), testUserID, int64(7), wantInput).Return(nil, err)
		}
	}
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).AddMember, http.MethodPost, "/teams/{id}/members", []teamTest{
		{
			name: "added", target: "/teams/7/members", body: body,
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().AddMember(gomock.Any(), testUserID, int64(7), wantInput).
					Return(&domain.MemberProfile{UserID: 9, Email: "nina@example.com", FullName: "Nina New", Role: domain.TeamRoleAdmin}, nil)
			},
			wantStatus: http.StatusCreated,
			wantBody:   `{"success":true,"data":{"user_id":9,"email":"nina@example.com","full_name":"Nina New","role":"admin"}}`,
		},
		{
			name: "role left to the default", target: "/teams/7/members", body: `{"email":"nina@example.com"}`,
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().AddMember(gomock.Any(), testUserID, int64(7), port.AddMemberInput{Email: "nina@example.com"}).
					Return(&domain.MemberProfile{UserID: 9, Email: "nina@example.com", FullName: "Nina New", Role: domain.TeamRoleMember}, nil)
			},
			wantStatus: http.StatusCreated,
			wantBody:   `{"success":true,"data":{"user_id":9,"email":"nina@example.com","full_name":"Nina New","role":"member"}}`,
		},
		{name: "malformed id", target: "/teams/x/members", body: body, wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{name: "unknown field", target: "/teams/7/members", body: `{"email":"nina@example.com","user_id":9}`, wantStatus: http.StatusBadRequest, wantBody: invalidBody},
		{
			name: "admin adding an admin", target: "/teams/7/members", body: body,
			expect:     failing(apperror.Forbidden("only the team owner may add admins")),
			wantStatus: http.StatusForbidden, wantBody: forbiddenBody("only the team owner may add admins"),
		},
		{
			name: "unknown email", target: "/teams/7/members", body: body,
			expect:     failing(apperror.NotFound("user", "nina@example.com")),
			wantStatus: http.StatusNotFound,
			wantBody:   `{"success":false,"error":{"code":"NOT_FOUND","message":"user not found","details":{"resource":"user","id":"nina@example.com"}}}`,
		},
		{
			name: "already a member", target: "/teams/7/members", body: body,
			expect:     failing(apperror.Conflict("team member", "user_id", int64(9))),
			wantStatus: http.StatusConflict,
			wantBody: `{"success":false,"error":{"code":"CONFLICT","message":"team member with this user_id already exists",` +
				`"details":{"resource":"team member","field":"user_id","value":9}}}`,
		},
	})
}

func TestTeamHandler_RemoveMember(t *testing.T) {
	t.Parallel()
	removing := func(err error) func(*mocks.MockTeamService) {
		return func(svc *mocks.MockTeamService) {
			svc.EXPECT().RemoveMember(gomock.Any(), testUserID, int64(7), int64(3)).Return(err)
		}
	}
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).RemoveMember, http.MethodDelete, "/teams/{id}/members/{user_id}", []teamTest{
		{name: "removed", target: "/teams/7/members/3", expect: removing(nil), wantStatus: http.StatusNoContent},
		{
			name: "forbidden", target: "/teams/7/members/3", expect: removing(apperror.Forbidden("admins may remove only members")),
			wantStatus: http.StatusForbidden, wantBody: forbiddenBody("admins may remove only members"),
		},
		{
			name: "not a member", target: "/teams/7/members/3", expect: removing(apperror.NotFound("team member", int64(3))),
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("team member", 3),
		},
		{name: "malformed team id", target: "/teams/-7/members/3", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{name: "malformed user id", target: "/teams/7/members/me", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("user_id", msgPositive)},
	})
}

func TestTeamHandler_Stats(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).Stats, http.MethodGet, "/teams/stats", []teamTest{
		{
			name: "stats of every team", target: "/teams/stats",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Stats(gomock.Any(), testUserID).Return([]domain.TeamStats{
					{TeamID: 7, Name: "Platform", MemberCount: 3, OpenTasks: 4, DoneLast7Days: 2},
				}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"success":true,"data":[{"team_id":7,"name":"Platform","member_count":3,"open_tasks":4,"done_last_7_days":2}]}`,
		},
		{
			name: "failure is not exposed", target: "/teams/stats",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().Stats(gomock.Any(), testUserID).Return(nil, apperror.Internal("team stats", errors.New("timeout")))
			},
			wantStatus: http.StatusInternalServerError, wantBody: internalError,
		},
	})
}

func TestTeamHandler_TopContributors(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTeamHandler, (*TeamHandler).TopContributors, http.MethodGet, "/teams/{id}/top-contributors", []teamTest{
		{
			name: "ties share a rank", target: "/teams/7/top-contributors",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().TopContributors(gomock.Any(), testUserID, int64(7)).Return([]domain.TopContributor{
					{UserID: 2, FullName: "Ada Admin", TasksCompleted: 5, Rank: 1},
					{UserID: 3, FullName: "Max Member", TasksCompleted: 5, Rank: 1},
				}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody: `{"success":true,"data":[{"user_id":2,"full_name":"Ada Admin","tasks_completed":5,"rank":1},` +
				`{"user_id":3,"full_name":"Max Member","tasks_completed":5,"rank":1}]}`,
		},
		{name: "malformed id", target: "/teams/7.5/top-contributors", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "team of others", target: "/teams/7/top-contributors",
			expect: func(svc *mocks.MockTeamService) {
				svc.EXPECT().TopContributors(gomock.Any(), testUserID, int64(7)).Return(nil, apperror.NotFound("team", int64(7)))
			},
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("team", 7),
		},
	})
}
