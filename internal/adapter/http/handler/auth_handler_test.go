package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

func testAuthResult() *port.AuthResult {
	return &port.AuthResult{
		Token:     "signed.access.token",
		ExpiresAt: time.Now().Add(time.Hour),
		User: domain.User{
			ID:           42,
			Email:        "alice@example.com",
			PasswordHash: "$2a$10$hash",
			FullName:     "Alice Example",
			CreatedAt:    time.Date(2026, 3, 14, 9, 26, 53, 0, time.FixedZone("UTC+3", 3*60*60)),
		},
	}
}

// wantAuthBody is the body for testAuthResult with expires_in left out, since it depends on
// the time the request is served.
const wantAuthBody = `{"success":true,"data":{"access_token":"signed.access.token","token_type":"Bearer",` +
	`"user":{"id":42,"email":"alice@example.com","full_name":"Alice Example","created_at":"2026-03-14T06:26:53Z"}}}`

// assertAuthBody compares body with wantAuthBody and checks expires_in separately.
func assertAuthBody(t *testing.T, body string) {
	t.Helper()
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	data, ok := got["data"].(map[string]any)
	require.True(t, ok, "body has data: %s", body)
	assert.InDelta(t, time.Hour.Seconds(), data["expires_in"], 1)
	delete(data, "expires_in")
	rest, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, wantAuthBody, string(rest))
}

func TestAuthHandler_Register(t *testing.T) {
	t.Parallel()
	body := `{"email":"Alice@Example.com","password":"correct horse","full_name":"Alice Example"}`
	wantInput := port.RegisterInput{Email: "Alice@Example.com", Password: "correct horse", FullName: "Alice Example"}
	tests := []struct {
		name       string
		body       string
		svcErr     error
		callsSvc   bool
		wantStatus int
		wantBody   string
	}{
		{name: "created", body: body, callsSvc: true, wantStatus: http.StatusCreated},
		{
			name: "malformed body", body: `{"email":`, wantStatus: http.StatusBadRequest,
			wantBody: `{"success":false,"error":{"code":"BAD_REQUEST","message":"invalid request body"}}`,
		},
		{
			name: "invalid input", body: body, callsSvc: true, wantStatus: http.StatusBadRequest,
			svcErr:   apperror.ValidationError("invalid input", map[string]any{"fields": map[string]string{"email": "is required"}}),
			wantBody: `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"invalid input","details":{"fields":{"email":"is required"}}}}`,
		},
		{
			name: "email taken", body: body, callsSvc: true, wantStatus: http.StatusConflict,
			svcErr: apperror.Conflict("user", "email", "alice@example.com"),
			wantBody: `{"success":false,"error":{"code":"CONFLICT","message":"user with this email already exists",` +
				`"details":{"resource":"user","field":"email","value":"alice@example.com"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockAuthService(gomock.NewController(t))
			if tt.callsSvc {
				res := testAuthResult()
				if tt.svcErr != nil {
					res = nil
				}
				svc.EXPECT().Register(gomock.Any(), wantInput).Return(res, tt.svcErr)
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/register", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			NewAuthHandler(svc).Register(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody == "" {
				assertAuthBody(t, rec.Body.String())
				return
			}
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestAuthHandler_Login(t *testing.T) {
	t.Parallel()
	body := `{"email":"alice@example.com","password":"correct horse"}`
	wantInput := port.LoginInput{Email: "alice@example.com", Password: "correct horse"}
	tests := []struct {
		name       string
		body       string
		svcErr     error
		callsSvc   bool
		wantStatus int
		wantBody   string
	}{
		{name: "logged in", body: body, callsSvc: true, wantStatus: http.StatusOK},
		{
			name: "malformed body", body: `[]`, wantStatus: http.StatusBadRequest,
			wantBody: `{"success":false,"error":{"code":"BAD_REQUEST","message":"invalid request body"}}`,
		},
		{
			name: "invalid credentials", body: body, callsSvc: true, wantStatus: http.StatusUnauthorized,
			svcErr:   apperror.Unauthorized("invalid email or password"),
			wantBody: `{"success":false,"error":{"code":"UNAUTHORIZED","message":"invalid email or password"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := mocks.NewMockAuthService(gomock.NewController(t))
			if tt.callsSvc {
				res := testAuthResult()
				if tt.svcErr != nil {
					res = nil
				}
				svc.EXPECT().Login(gomock.Any(), wantInput).Return(res, tt.svcErr)
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/login", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			NewAuthHandler(svc).Login(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody == "" {
				assertAuthBody(t, rec.Body.String())
				return
			}
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}
