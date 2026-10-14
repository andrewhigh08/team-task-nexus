package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

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
	svc := mocks.NewMockTaskService(gomock.NewController(t))
	svc.EXPECT().Create(gomock.Any(), testUserID, port.CreateTaskInput{
		TeamID: 7, Title: "Rotate the certificates", Description: "Before they expire",
		Priority: new(domain.TaskPriorityHigh), AssigneeID: new(int64(3)), DueDate: &dueDate,
	}).Return(testTask(), nil)
	body := `{"team_id":7,"title":"Rotate the certificates","description":"Before they expire","priority":3,` +
		`"assignee_id":3,"due_date":"2026-04-01"}`

	rec := serve(t, NewTaskHandler(svc).Create, http.MethodPost, "/tasks", "/tasks", body)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":`+testTaskJSON+`}`, rec.Body.String())
}

func TestTaskHandler_Create_RejectsMalformedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed due date", body: `{"team_id":7,"title":"t","due_date":"01.04.2026"}`, want: invalidFieldBody("due_date", "must be a date in YYYY-MM-DD format")},
		{name: "unknown field", body: `{"team_id":7,"title":"t","status":"done"}`, want: `{"success":false,"error":{"code":"BAD_REQUEST","message":"invalid request body"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockTaskService(gomock.NewController(t))

			rec := serve(t, NewTaskHandler(svc).Create, http.MethodPost, "/tasks", "/tasks", tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

func TestTaskHandler_List(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
		query  domain.TaskQuery
		page   *domain.TaskPage
		want   string
	}{
		{
			name:   "every parameter",
			target: "/tasks?team_id=7&status=done&assignee_id=3&page=2&page_size=1",
			query:  domain.TaskQuery{ViewerID: testUserID, TeamID: 7, Status: domain.TaskStatusDone, AssigneeID: 3, Page: 2, PageSize: 1},
			page:   &domain.TaskPage{Tasks: []domain.Task{*testTask(), openTask()}, Total: 3, Page: 2, PageSize: 1, TotalPages: 3},
			want: `{"success":true,"data":[` + testTaskJSON + `,` + openTaskJSON + `],` +
				`"meta":{"page":2,"page_size":1,"total":3,"total_pages":3}}`,
		},
		{
			name:   "no parameters, no tasks",
			target: "/tasks",
			query:  domain.TaskQuery{ViewerID: testUserID},
			page:   &domain.TaskPage{Page: 1, PageSize: 20},
			want:   `{"success":true,"data":[],"meta":{"page":1,"page_size":20,"total":0,"total_pages":0}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockTaskService(gomock.NewController(t))
			svc.EXPECT().List(gomock.Any(), tt.query).Return(tt.page, nil)

			rec := serve(t, NewTaskHandler(svc).List, http.MethodGet, "/tasks", tt.target, "")

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

func TestTaskHandler_List_RejectsMalformedParameters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target string
		field  string
	}{
		{target: "/tasks?team_id=abc", field: "team_id"},
		{target: "/tasks?assignee_id=0", field: "assignee_id"},
		{target: "/tasks?page=0", field: "page"},
		{target: "/tasks?page_size=-5", field: "page_size"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockTaskService(gomock.NewController(t))

			rec := serve(t, NewTaskHandler(svc).List, http.MethodGet, "/tasks", tt.target, "")

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, invalidFieldBody(tt.field, msgPositive), rec.Body.String())
		})
	}
}

func TestTaskHandler_Update(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want port.UpdateTaskInput
	}{
		{name: "absent fields stay absent", body: `{}`},
		{
			name: "values",
			body: `{"title":"Renew","description":"","status":"review","priority":1,"assignee_id":3,"due_date":"2026-04-01"}`,
			want: port.UpdateTaskInput{
				Title:       opt.Of("Renew"),
				Description: opt.Of(""),
				Status:      opt.Of(domain.TaskStatusReview),
				Priority:    opt.Of(domain.TaskPriorityLow),
				AssigneeID:  opt.Of[int64](3),
				DueDate:     opt.Of(dueDate),
			},
		},
		{
			name: "nulls are passed on for the service to accept or reject",
			body: `{"title":null,"assignee_id":null,"due_date":null}`,
			want: port.UpdateTaskInput{Title: opt.Null[string](), AssigneeID: opt.Null[int64](), DueDate: opt.Null[time.Time]()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockTaskService(gomock.NewController(t))
			svc.EXPECT().Update(gomock.Any(), testUserID, int64(11), tt.want).Return(testTask(), nil)

			rec := serve(t, NewTaskHandler(svc).Update, http.MethodPatch, "/tasks/{id}", "/tasks/11", tt.body)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, `{"success":true,"data":`+testTaskJSON+`}`, rec.Body.String())
		})
	}
}

func TestTaskHandler_Update_Rejections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		target     string
		body       string
		svcErr     error
		wantStatus int
		want       string
	}{
		{
			name: "malformed id", target: "/tasks/abc", body: `{}`,
			wantStatus: http.StatusBadRequest, want: invalidFieldBody("id", msgPositive),
		},
		{
			name: "malformed due date", target: "/tasks/11", body: `{"due_date":"tomorrow"}`,
			wantStatus: http.StatusBadRequest, want: invalidFieldBody("due_date", "must be a date in YYYY-MM-DD format"),
		},
		{
			name: "status of the wrong type", target: "/tasks/11", body: `{"status":3}`,
			wantStatus: http.StatusBadRequest, want: `{"success":false,"error":{"code":"BAD_REQUEST","message":"invalid request body"}}`,
		},
		{
			name: "task of another team", target: "/tasks/11", body: `{"title":"x"}`, svcErr: apperror.NotFound("task", int64(11)),
			wantStatus: http.StatusNotFound,
			want:       `{"success":false,"error":{"code":"NOT_FOUND","message":"task not found","details":{"resource":"task","id":11}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockTaskService(gomock.NewController(t))
			if tt.svcErr != nil {
				svc.EXPECT().Update(gomock.Any(), testUserID, int64(11), gomock.Any()).Return(nil, tt.svcErr)
			}

			rec := serve(t, NewTaskHandler(svc).Update, http.MethodPatch, "/tasks/{id}", tt.target, tt.body)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

func TestTaskHandler_History(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockTaskService(gomock.NewController(t))
	svc.EXPECT().History(gomock.Any(), testUserID, int64(11)).Return([]domain.TaskChange{{
		ID: 5, TaskID: 11, UserID: 3, Field: "assignee_id", OldValue: "", NewValue: "3", ChangedAt: completedAt,
	}}, nil)

	rec := serve(t, NewTaskHandler(svc).History, http.MethodGet, "/tasks/{id}/history", "/tasks/11/history", "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":[{"id":5,"task_id":11,"user_id":3,"field":"assignee_id","old_value":"",`+
		`"new_value":"3","changed_at":"2026-03-14T09:26:53Z"}]}`, rec.Body.String())
}

func TestTaskHandler_ListOrphaned(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockTaskService(gomock.NewController(t))
	svc.EXPECT().ListOrphaned(gomock.Any(), testUserID, int64(7)).Return([]domain.OrphanedAssignment{{
		TaskID: 11, TaskTitle: "Rotate the certificates", AssigneeID: 9, AssigneeName: "Former Member",
	}}, nil)

	rec := serve(t, NewTaskHandler(svc).ListOrphaned, http.MethodGet, "/teams/{id}/orphaned-assignees", "/teams/7/orphaned-assignees", "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"success":true,"data":[{"task_id":11,"task_title":"Rotate the certificates",`+
		`"assignee_id":9,"assignee_name":"Former Member"}]}`, rec.Body.String())
}
