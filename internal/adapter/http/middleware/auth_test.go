package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const (
	testToken        = "header.payload.signature"
	testUserID int64 = 7
)

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	errRejected := apperror.Unauthorized("invalid or expired token")
	tests := []struct {
		name        string
		headers     []string
		wantToken   string // token handed to the verifier; empty means the verifier is not called
		verifyErr   error
		wantStatus  int
		wantMessage string
	}{
		{name: "valid token", headers: []string{"Bearer " + testToken}, wantToken: testToken, wantStatus: http.StatusNoContent},
		{name: "lower case scheme", headers: []string{"bearer " + testToken}, wantToken: testToken, wantStatus: http.StatusNoContent},
		{name: "upper case scheme", headers: []string{"BEARER " + testToken}, wantToken: testToken, wantStatus: http.StatusNoContent},
		{name: "surrounding spaces", headers: []string{"  Bearer   " + testToken + " "}, wantToken: testToken, wantStatus: http.StatusNoContent},
		{name: "missing header", wantStatus: http.StatusUnauthorized, wantMessage: "authentication required"},
		{name: "empty header", headers: []string{""}, wantStatus: http.StatusUnauthorized, wantMessage: "invalid authorization header"},
		{name: "other scheme", headers: []string{"Basic dXNlcjpwYXNz"}, wantStatus: http.StatusUnauthorized, wantMessage: "invalid authorization header"},
		{name: "scheme without token", headers: []string{"Bearer"}, wantStatus: http.StatusUnauthorized, wantMessage: "invalid authorization header"},
		{name: "token without scheme", headers: []string{testToken}, wantStatus: http.StatusUnauthorized, wantMessage: "invalid authorization header"},
		{
			name: "two tokens", headers: []string{"Bearer " + testToken + " " + testToken},
			wantStatus: http.StatusUnauthorized, wantMessage: "invalid authorization header",
		},
		{
			name: "two headers", headers: []string{"Bearer " + testToken, "Bearer " + testToken},
			wantStatus: http.StatusUnauthorized, wantMessage: "invalid authorization header",
		},
		{
			name: "rejected token", headers: []string{"Bearer " + testToken}, wantToken: testToken, verifyErr: errRejected,
			wantStatus: http.StatusUnauthorized, wantMessage: "invalid or expired token",
		},
		{
			name: "verifier failure is not exposed", headers: []string{"Bearer " + testToken}, wantToken: testToken,
			verifyErr: errors.New("unexpected"), wantStatus: http.StatusUnauthorized, wantMessage: "invalid or expired token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			verifier := mocks.NewMockTokenVerifier(gomock.NewController(t))
			if tt.wantToken != "" {
				var userID int64
				if tt.verifyErr == nil {
					userID = testUserID
				}
				verifier.EXPECT().Verify(tt.wantToken).Return(userID, tt.verifyErr)
			}
			var called bool
			var gotUserID, gotLogUserID int64
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				gotUserID, _ = middleware.UserIDFrom(r.Context())
				gotLogUserID = logger.UserIDFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/teams", http.NoBody)
			for _, h := range tt.headers {
				req.Header.Add("Authorization", h)
			}
			rec := httptest.NewRecorder()

			middleware.Authenticate(verifier)(next).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus != http.StatusUnauthorized {
				require.True(t, called)
				assert.Equal(t, testUserID, gotUserID)
				assert.Equal(t, testUserID, gotLogUserID, "the user id reaches the request logger")
				return
			}
			assert.False(t, called, "a rejected request never reaches the handler")
			assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
			assert.JSONEq(t,
				`{"success":false,"error":{"code":"UNAUTHORIZED","message":"`+tt.wantMessage+`"}}`,
				rec.Body.String())
		})
	}
}

func TestUserIDFrom(t *testing.T) {
	t.Parallel()

	t.Run("without a user", func(t *testing.T) {
		t.Parallel()
		userID, ok := middleware.UserIDFrom(t.Context())

		assert.False(t, ok)
		assert.Zero(t, userID)
	})

	t.Run("with a user", func(t *testing.T) {
		t.Parallel()
		ctx := middleware.WithUserID(t.Context(), 42)

		userID, ok := middleware.UserIDFrom(ctx)

		assert.True(t, ok)
		assert.Equal(t, int64(42), userID)
		assert.Equal(t, int64(42), logger.UserIDFromContext(ctx), "the request logger sees the same user")
	})
}
