package handler

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

type commentTest = handlerTest[*mocks.MockCommentService]

// mockedCommentHandler returns a CommentHandler on a mock service prepared by expect.
func mockedCommentHandler(t *testing.T, expect func(*mocks.MockCommentService)) *CommentHandler {
	svc := mocks.NewMockCommentService(gomock.NewController(t))
	if expect != nil {
		expect(svc)
	}
	return NewCommentHandler(svc)
}

func testComment() *domain.Comment {
	created := time.Date(2026, 3, 14, 12, 26, 53, 0, time.FixedZone("UTC+3", 3*60*60))
	return &domain.Comment{ID: 21, TaskID: 11, UserID: testUserID, Content: "Renewed until 2027", CreatedAt: created, UpdatedAt: created}
}

const testCommentJSON = `{"id":21,"task_id":11,"user_id":42,"content":"Renewed until 2027",` +
	`"created_at":"2026-03-14T09:26:53Z","updated_at":"2026-03-14T09:26:53Z"}`

func TestCommentHandler_Create(t *testing.T) {
	t.Parallel()
	body := `{"content":"Renewed until 2027"}`
	creating := func(comment *domain.Comment, err error) func(*mocks.MockCommentService) {
		return func(svc *mocks.MockCommentService) {
			svc.EXPECT().Create(gomock.Any(), testUserID, int64(11), port.CreateCommentInput{Content: "Renewed until 2027"}).
				Return(comment, err)
		}
	}
	runHandlerTests(t, mockedCommentHandler, (*CommentHandler).Create, http.MethodPost, "/tasks/{id}/comments", []commentTest{
		{
			name: "created", target: "/tasks/11/comments", body: body, expect: creating(testComment(), nil),
			wantStatus: http.StatusCreated, wantBody: `{"success":true,"data":` + testCommentJSON + `}`,
		},
		{name: "malformed id", target: "/tasks/x/comments", body: body, wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{name: "malformed body", target: "/tasks/11/comments", body: `"Renewed"`, wantStatus: http.StatusBadRequest, wantBody: invalidBody},
		{
			name: "content too long", target: "/tasks/11/comments", body: body,
			expect: creating(nil, apperror.ValidationError("invalid input", map[string]any{
				"fields": map[string]string{"content": "must be at most 5000 characters"},
			})),
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("content", "must be at most 5000 characters"),
		},
		{
			name: "task of another team", target: "/tasks/11/comments", body: body, expect: creating(nil, apperror.NotFound("task", int64(11))),
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("task", 11),
		},
	})
}

func TestCommentHandler_List(t *testing.T) {
	t.Parallel()
	listing := func(p domain.PageRequest, page *domain.CommentPage, err error) func(*mocks.MockCommentService) {
		return func(svc *mocks.MockCommentService) {
			svc.EXPECT().List(gomock.Any(), testUserID, int64(11), p).Return(page, err)
		}
	}
	runHandlerTests(t, mockedCommentHandler, (*CommentHandler).List, http.MethodGet, "/tasks/{id}/comments", []commentTest{
		{
			name: "requested page", target: "/tasks/11/comments?page=3&page_size=2",
			expect: listing(domain.PageRequest{Page: 3, PageSize: 2},
				&domain.CommentPage{Comments: []domain.Comment{*testComment()}, Total: 5, Page: 3, PageSize: 2, TotalPages: 3}, nil),
			wantStatus: http.StatusOK,
			wantBody:   `{"success":true,"data":[` + testCommentJSON + `],"meta":{"page":3,"page_size":2,"total":5,"total_pages":3}}`,
		},
		{
			name: "no parameters, no comments", target: "/tasks/11/comments",
			expect:     listing(domain.PageRequest{}, &domain.CommentPage{Page: 1, PageSize: 20}, nil),
			wantStatus: http.StatusOK,
			wantBody:   `{"success":true,"data":[],"meta":{"page":1,"page_size":20,"total":0,"total_pages":0}}`,
		},
		{
			name: "page above the maximum", target: "/tasks/11/comments?page=10001",
			expect: listing(domain.PageRequest{Page: 10001}, nil, apperror.ValidationError("invalid input",
				map[string]any{"fields": map[string]string{"page": "must be between 1 and 10000"}})),
			wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("page", "must be between 1 and 10000"),
		},
		{name: "malformed id", target: "/tasks/-1/comments", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("id", msgPositive)},
		{name: "malformed page", target: "/tasks/11/comments?page=first", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("page", msgPositive)},
		{name: "malformed page size", target: "/tasks/11/comments?page_size=-5", wantStatus: http.StatusBadRequest, wantBody: invalidFieldBody("page_size", msgPositive)},
		{
			name: "task of another team", target: "/tasks/11/comments",
			expect:     listing(domain.PageRequest{}, nil, apperror.NotFound("task", int64(11))),
			wantStatus: http.StatusNotFound, wantBody: notFoundBody("task", 11),
		},
	})
}
