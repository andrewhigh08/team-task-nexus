package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
)

func TestRequireUserID(t *testing.T) {
	t.Parallel()

	t.Run("authenticated", func(t *testing.T) {
		t.Parallel()
		ctx := middleware.WithUserID(t.Context(), 42)
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/teams", http.NoBody)
		rec := httptest.NewRecorder()

		userID, ok := requireUserID(rec, req)

		assert.True(t, ok)
		assert.Equal(t, int64(42), userID)
		assert.Zero(t, rec.Body.Len(), "nothing is written for the handler to overwrite")
	})

	t.Run("anonymous", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/teams", http.NoBody)
		rec := httptest.NewRecorder()

		_, ok := requireUserID(rec, req)

		assert.False(t, ok)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.JSONEq(t, `{"success":false,"error":{"code":"UNAUTHORIZED","message":"authentication required"}}`, rec.Body.String())
	})
}
