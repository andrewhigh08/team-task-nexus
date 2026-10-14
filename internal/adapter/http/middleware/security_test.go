package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
)

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path        string
		wantNoStore bool
	}{
		{path: "/api/v1/tasks", wantNoStore: true},
		{path: "/api", wantNoStore: true},
		{path: "/apis"},
		{path: "/health/ready"},
		{path: "/openapi.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			h := middleware.SecurityHeaders(http.HandlerFunc(noContent))

			rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody))

			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
			assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
			if tt.wantNoStore {
				assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				return
			}
			assert.Empty(t, rec.Header().Get("Cache-Control"))
		})
	}
}
