package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

func testTeam() domain.Team {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	return domain.Team{ID: 7, Name: "Platform", Description: "Keeps the lights on", OwnerID: 1, CreatedAt: created, UpdatedAt: created}
}

const testTeamJSON = `{"id":7,"name":"Platform","description":"Keeps the lights on","owner_id":1,` +
	`"created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-01T09:00:00Z"}`

func TestTeamHandler_Create(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockTeamService(gomock.NewController(t))
	team := testTeam()
	svc.EXPECT().Create(gomock.Any(), testUserID, port.CreateTeamInput{Name: "Platform", Description: "Keeps the lights on"}).
		Return(&team, nil)

	rec := serve(t, NewTeamHandler(svc).Create, http.MethodPost, "/teams", "/teams", `{"name":"Platform","description":"Keeps the lights on"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":`+testTeamJSON+`}`, rec.Body.String())
}

func TestTeamHandler_List(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockTeamService(gomock.NewController(t))
	svc.EXPECT().List(gomock.Any(), testUserID).Return([]domain.TeamSummary{{Team: testTeam(), Role: domain.TeamRoleAdmin}}, nil)

	rec := serve(t, NewTeamHandler(svc).List, http.MethodGet, "/teams", "/teams", "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":[{"id":7,"name":"Platform","description":"Keeps the lights on","owner_id":1,`+
		`"created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-01T09:00:00Z","role":"admin"}]}`, rec.Body.String())
}

func TestTeamHandler_ListMembers_OfAnotherTeam(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockTeamService(gomock.NewController(t))
	svc.EXPECT().ListMembers(gomock.Any(), testUserID, int64(7)).Return(nil, apperror.NotFound("team", int64(7)))

	rec := serve(t, NewTeamHandler(svc).ListMembers, http.MethodGet, "/teams/{id}/members", "/teams/7/members", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"NOT_FOUND","message":"team not found","details":{"resource":"team","id":7}}}`,
		rec.Body.String())
}

func TestTeamHandler_AddMember(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockTeamService(gomock.NewController(t))
	svc.EXPECT().AddMember(gomock.Any(), testUserID, int64(7), port.AddMemberInput{Email: "nina@example.com", Role: domain.TeamRoleAdmin}).
		Return(&domain.MemberProfile{UserID: 9, Email: "nina@example.com", FullName: "Nina New", Role: domain.TeamRoleAdmin}, nil)

	rec := serve(t, NewTeamHandler(svc).AddMember, http.MethodPost, "/teams/{id}/members", "/teams/7/members",
		`{"email":"nina@example.com","role":"admin"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":{"user_id":9,"email":"nina@example.com","full_name":"Nina New","role":"admin"}}`,
		rec.Body.String())
}

func TestTeamHandler_RemoveMember(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		target     string
		svcErr     error
		callsSvc   bool
		wantStatus int
		wantBody   string
	}{
		{name: "removed", target: "/teams/7/members/3", callsSvc: true, wantStatus: http.StatusNoContent},
		{
			name: "forbidden", target: "/teams/7/members/3", callsSvc: true, svcErr: apperror.Forbidden("admins may remove only members"),
			wantStatus: http.StatusForbidden,
			wantBody:   `{"success":false,"error":{"code":"FORBIDDEN","message":"admins may remove only members"}}`,
		},
		{
			name: "malformed user id", target: "/teams/7/members/me",
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("user_id", msgPositive),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockTeamService(gomock.NewController(t))
			if tt.callsSvc {
				svc.EXPECT().RemoveMember(gomock.Any(), testUserID, int64(7), int64(3)).Return(tt.svcErr)
			}

			rec := serve(t, NewTeamHandler(svc).RemoveMember, http.MethodDelete, "/teams/{id}/members/{user_id}", tt.target, "")

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody == "" {
				assert.Empty(t, rec.Body.String())
				return
			}
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestTeamHandler_Analytics(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	svc := mocks.NewMockTeamService(ctrl)
	svc.EXPECT().Stats(gomock.Any(), testUserID).Return([]domain.TeamStats{
		{TeamID: 7, Name: "Platform", MemberCount: 3, OpenTasks: 4, DoneLast7Days: 2},
	}, nil)
	svc.EXPECT().TopContributors(gomock.Any(), testUserID, int64(7)).Return([]domain.TopContributor{
		{UserID: 2, FullName: "Ada Admin", TasksCompleted: 5, Rank: 1},
		{UserID: 3, FullName: "Max Member", TasksCompleted: 5, Rank: 1},
	}, nil)
	h := NewTeamHandler(svc)

	stats := serve(t, h.Stats, http.MethodGet, "/teams/stats", "/teams/stats", "")
	top := serve(t, h.TopContributors, http.MethodGet, "/teams/{id}/top-contributors", "/teams/7/top-contributors", "")

	assert.Equal(t, http.StatusOK, stats.Code)
	assert.JSONEq(t, `{"success":true,"data":[{"team_id":7,"name":"Platform","member_count":3,"open_tasks":4,"done_last_7_days":2}]}`,
		stats.Body.String())
	assert.Equal(t, http.StatusOK, top.Code)
	assert.JSONEq(t, `{"success":true,"data":[{"user_id":2,"full_name":"Ada Admin","tasks_completed":5,"rank":1},`+
		`{"user_id":3,"full_name":"Max Member","tasks_completed":5,"rank":1}]}`, top.Body.String())
}
