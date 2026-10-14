package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
)

func TestBodyLimit(t *testing.T) {
	t.Parallel()
	const limit = 8
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "below the limit", body: "1234567"},
		{name: "at the limit", body: "12345678"},
		{name: "over the limit", body: "123456789", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var (
				read    []byte
				readErr error
			)
			h := middleware.BodyLimit(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				read, readErr = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))

			serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body)))

			if !tt.wantErr {
				require.NoError(t, readErr)
				assert.Equal(t, tt.body, string(read))
				return
			}
			var tooLarge *http.MaxBytesError
			require.ErrorAs(t, readErr, &tooLarge)
			assert.Equal(t, int64(limit), tooLarge.Limit)
		})
	}
}
