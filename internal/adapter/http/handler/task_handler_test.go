package handler

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

type taskTest = handlerTest[*mocks.MockTaskService]

// mockedTaskHandler returns a TaskHandler on a mock service prepared by expect.
func mockedTaskHandler(t *testing.T, expect func(*mocks.MockTaskService)) *TaskHandler {
	svc := mocks.NewMockTaskService(gomock.NewController(t))
	if expect != nil {
		expect(svc)
	}
	return NewTaskHandler(svc)
}

var (
	dueDate     = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	completedAt = time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
)

// testTask is a task with every optional field set; its timestamps are not in UTC, so the
// tests can tell that responses convert them.
func testTask() *domain.Task {
	moscow := time.FixedZone("UTC+3", 3*60*60)
	return &domain.Task{
		ID:          11,
		TeamID:      7,
		Title:       "Rotate the certificates",
		Description: "Before they expire",
		Status:      domain.TaskStatusDone,
		Priority:    domain.TaskPriorityHigh,
		CreatorID:   1,
		AssigneeID:  new(int64(3)),
		DueDate:     &dueDate,
		CompletedAt: new(completedAt.In(moscow)),
		CreatedAt:   time.Date(2026, 3, 1, 12, 0, 0, 0, moscow),
		UpdatedAt:   time.Date(2026, 3, 14, 12, 26, 53, 0, moscow),
	}
}

const testTaskJSON = `{"id":11,"team_id":7,"title":"Rotate the certificates","description":"Before they expire",` +
	`"status":"done","priority":3,"creator_id":1,"assignee_id":3,"due_date":"2026-04-01",` +
	`"completed_at":"2026-03-14T09:26:53Z","created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-14T09:26:53Z"}`

const openTaskJSON = `{"id":12,"team_id":7,"title":"Open","description":"","status":"todo","priority":2,"creator_id":1,` +
	`"assignee_id":null,"due_date":null,"completed_at":null,"created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-01T09:00:00Z"}`

func openTask() domain.Task {
	created := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	return domain.Task{
		ID: 12, TeamID: 7, Title: "Open", Status: domain.TaskStatusTodo, Priority: domain.TaskPriorityMedium,
		CreatorID: 1, CreatedAt: created, UpdatedAt: created,
	}
}

func TestTaskHandler_Create(t *testing.T) {
	t.Parallel()
	body := `{"team_id":7,"title":"Rotate the certificates","description":"Before they expire","priority":3,` +
		`"assignee_id":3,"due_date":"2026-04-01"}`
	wantInput := port.CreateTaskInput{
		TeamID: 7, Title: "Rotate the certificates", Description: "Before they expire",
		Priority: new(domain.TaskPriorityHigh), AssigneeID: new(int64(3)), DueDate: &dueDate,
	}
	failing := func(err error) func(*mocks.MockTaskService) {
		return func(svc *mocks.MockTaskService) {
			svc.EXPECT().Create(gomock.Any(), testUserID, wantInput).Return(nil, err)
		}
	}
	runHandlerTests(t, mockedTaskHandler, (*TaskHandler).Create, http.MethodPost, "/tasks", []taskTest{
		{
			name: "created", target: "/tasks", body: body,
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().Create(gomock.Any(), testUserID, wantInput).Return(testTask(), nil)
			},
			wantStatus: http.StatusCreated, wantBody: `{"success":true,"data":` + testTaskJSON + `}`,
		},
		{
			name: "optional fields left out", target: "/tasks", body: `{"team_id":7,"title":"Open"}`,
			expect: func(svc *mocks.MockTaskService) {
				open := openTask()
				svc.EXPECT().Create(gomock.Any(), testUserID, port.CreateTaskInput{TeamID: 7, Title: "Open"}).Return(&open, nil)
			},
			wantStatus: http.StatusCreated, wantBody: `{"success":true,"data":` + openTaskJSON + `}`,
		},
		{
			name: "malformed due date", target: "/tasks", body: `{"team_id":7,"title":"t","due_date":"01.04.2026"}`,
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("due_date", "must be a date in YYYY-MM-DD format"),
		},
		{name: "unknown field", target: "/tasks", body: `{"team_id":7,"title":"t","status":"done"}`, wantStatus: http.StatusBadRequest, wantBody: invalidBody},
		{
			name: "assignee outside the team", target: "/tasks", body: body,
			expect: failing(apperror.ValidationError("invalid input", map[string]any{
				"fields": map[string]string{"assignee_id": "must be a member of the team"},
			})),
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("assignee_id", "must be a member of the team"),
		},
		{
			name: "team of others", target: "/tasks", body: body, expect: failing(apperror.NotFound("team", int64(7))),
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("team", 7),
		},
	})
}

func TestTaskHandler_List(t *testing.T) {
	t.Parallel()
	listing := func(q domain.TaskQuery, page *domain.TaskPage, err error) func(*mocks.MockTaskService) {
		return func(svc *mocks.MockTaskService) { svc.EXPECT().List(gomock.Any(), q).Return(page, err) }
	}
	runHandlerTests(t, mockedTaskHandler, (*TaskHandler).List, http.MethodGet, "/tasks", []taskTest{
		{
			name:   "every parameter",
			target: "/tasks?team_id=7&status=done&assignee_id=3&page=2&page_size=1",
			expect: listing(
				domain.TaskQuery{ViewerID: testUserID, TeamID: 7, Status: domain.TaskStatusDone, AssigneeID: 3, Page: 2, PageSize: 1},
				&domain.TaskPage{Tasks: []domain.Task{*testTask(), openTask()}, Total: 3, Page: 2, PageSize: 1, TotalPages: 3}, nil,
			),
			wantStatus: http.StatusOK,
			wantBody: `{"success":true,"data":[` + testTaskJSON + `,` + openTaskJSON + `],` +
				`"meta":{"page":2,"page_size":1,"total":3,"total_pages":3}}`,
		},
		{
			name:       "no parameters, no tasks",
			target:     "/tasks",
			expect:     listing(domain.TaskQuery{ViewerID: testUserID}, &domain.TaskPage{Page: 1, PageSize: 20}, nil),
			wantStatus: http.StatusOK,
			wantBody:   `{"success":true,"data":[],"meta":{"page":1,"page_size":20,"total":0,"total_pages":0}}`,
		},
		{
			name:   "unknown status",
			target: "/tasks?status=blocked",
			expect: listing(domain.TaskQuery{ViewerID: testUserID, Status: "blocked"}, nil, apperror.ValidationError("invalid input",
				map[string]any{"fields": map[string]string{"status": "must be one of todo, in_progress, review, done"}})),
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("status", "must be one of todo, in_progress, review, done"),
		},
		{
			name:       "team of others",
			target:     "/tasks?team_id=8",
			expect:     listing(domain.TaskQuery{ViewerID: testUserID, TeamID: 8}, nil, apperror.NotFound("team", int64(8))),
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("team", 8),
		},
		{name: "malformed team id", target: "/tasks?team_id=abc", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("team_id", msgPositive)},
		{name: "malformed assignee id", target: "/tasks?assignee_id=0", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("assignee_id", msgPositive)},
		{name: "malformed page", target: "/tasks?page=0", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("page", msgPositive)},
		{name: "malformed page size", target: "/tasks?page_size=-5", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("page_size", msgPositive)},
	})
}

func TestTaskHandler_Get(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTaskHandler, (*TaskHandler).Get, http.MethodGet, "/tasks/{id}", []taskTest{
		{
			name: "found", target: "/tasks/11",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().Get(gomock.Any(), testUserID, int64(11)).Return(testTask(), nil)
			},
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":` + testTaskJSON + `}`,
		},
		{name: "malformed id", target: "/tasks/eleven", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "task of another team", target: "/tasks/11",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().Get(gomock.Any(), testUserID, int64(11)).Return(nil, apperror.NotFound("task", int64(11)))
			},
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("task", 11),
		},
	})
}

func TestTaskHandler_Update(t *testing.T) {
	t.Parallel()
	updating := func(in port.UpdateTaskInput) func(*mocks.MockTaskService) {
		return func(svc *mocks.MockTaskService) {
			svc.EXPECT().Update(gomock.Any(), testUserID, int64(11), in).Return(testTask(), nil)
		}
	}
	runHandlerTests(t, mockedTaskHandler, (*TaskHandler).Update, http.MethodPatch, "/tasks/{id}", []taskTest{
		{
			name: "absent fields stay absent", target: "/tasks/11", body: `{}`, expect: updating(port.UpdateTaskInput{}),
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":` + testTaskJSON + `}`,
		},
		{
			name:   "values",
			target: "/tasks/11",
			body:   `{"title":"Renew","description":"","status":"review","priority":1,"assignee_id":3,"due_date":"2026-04-01"}`,
			expect: updating(port.UpdateTaskInput{
				Title:       opt.Of("Renew"),
				Description: opt.Of(""),
				Status:      opt.Of(domain.TaskStatusReview),
				Priority:    opt.Of(domain.TaskPriorityLow),
				AssigneeID:  opt.Of[int64](3),
				DueDate:     opt.Of(dueDate),
			}),
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":` + testTaskJSON + `}`,
		},
		{
			name:       "nulls are passed on for the service to accept or reject",
			target:     "/tasks/11",
			body:       `{"title":null,"assignee_id":null,"due_date":null}`,
			expect:     updating(port.UpdateTaskInput{Title: opt.Null[string](), AssigneeID: opt.Null[int64](), DueDate: opt.Null[time.Time]()}),
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":` + testTaskJSON + `}`,
		},
		{name: "malformed id", target: "/tasks/abc", body: `{}`, wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "malformed due date", target: "/tasks/11", body: `{"due_date":"tomorrow"}`,
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("due_date", "must be a date in YYYY-MM-DD format"),
		},
		{name: "status of the wrong type", target: "/tasks/11", body: `{"status":3}`, wantStatus: http.StatusBadRequest, wantBody: invalidBody},
		{
			name: "task of another team", target: "/tasks/11", body: `{"title":"x"}`,
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().Update(gomock.Any(), testUserID, int64(11), port.UpdateTaskInput{Title: opt.Of("x")}).
					Return(nil, apperror.NotFound("task", int64(11)))
			},
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("task", 11),
		},
	})
}

func TestTaskHandler_History(t *testing.T) {
	t.Parallel()
	runHandlerTests(t, mockedTaskHandler, (*TaskHandler).History, http.MethodGet, "/tasks/{id}/history", []taskTest{
		{
			name: "changes", target: "/tasks/11/history",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().History(gomock.Any(), testUserID, int64(11)).Return([]domain.TaskChange{{
					ID: 5, TaskID: 11, UserID: 3, Field: "assignee_id", OldValue: "", NewValue: "3", ChangedAt: completedAt,
				}}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody: `{"success":true,"data":[{"id":5,"task_id":11,"user_id":3,"field":"assignee_id","old_value":"",` +
				`"new_value":"3","changed_at":"2026-03-14T09:26:53Z"}]}`,
		},
		{
			name: "no changes", target: "/tasks/11/history",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().History(gomock.Any(), testUserID, int64(11)).Return(nil, nil)
			},
			wantStatus: http.StatusOK, wantBody: `{"success":true,"data":[]}`,
		},
		{name: "malformed id", target: "/tasks/0/history", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "task of another team", target: "/tasks/11/history",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().History(gomock.Any(), testUserID, int64(11)).Return(nil, apperror.NotFound("task", int64(11)))
			},
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("task", 11),
		},
	})
}

func TestTaskHandler_ListOrphaned(t *testing.T) {
	t.Parallel()
	pattern := "/teams/{id}/orphaned-assignees"
	runHandlerTests(t, mockedTaskHandler, (*TaskHandler).ListOrphaned, http.MethodGet, pattern, []taskTest{
		{
			name: "orphaned assignments", target: "/teams/7/orphaned-assignees",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().ListOrphaned(gomock.Any(), testUserID, int64(7)).Return([]domain.OrphanedAssignment{{
					TaskID: 11, TaskTitle: "Rotate the certificates", AssigneeID: 9, AssigneeName: "Former Member",
				}}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody: `{"success":true,"data":[{"task_id":11,"task_title":"Rotate the certificates",` +
				`"assignee_id":9,"assignee_name":"Former Member"}]}`,
		},
		{name: "malformed id", target: "/teams/team/orphaned-assignees", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{
			name: "plain member", target: "/teams/7/orphaned-assignees",
			expect: func(svc *mocks.MockTaskService) {
				svc.EXPECT().ListOrphaned(gomock.Any(), testUserID, int64(7)).
					Return(nil, apperror.Forbidden("only the team owner and admins may do this"))
			},
			wantStatus: http.StatusForbidden, wantBody: forbiddenBody("only the team owner and admins may do this"),
		},
	})
}
