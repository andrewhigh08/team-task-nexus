package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

var generatedID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// requestIDs serves req through RequestID and returns the id sent back in the response and
// the one the handler found in the request context.
func requestIDs(req *http.Request) (header, inContext string) {
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inContext = logger.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := serve(h, req)
	return rec.Header().Get(middleware.RequestIDHeader), inContext
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		supplied string
		accepted bool
	}{
		{name: "missing", supplied: ""},
		{name: "letters, digits, dots, underscores and dashes", supplied: "req-1.abc_DEF", accepted: true},
		{name: "64 characters", supplied: strings.Repeat("a", 64), accepted: true},
		{name: "65 characters", supplied: strings.Repeat("a", 65)},
		{name: "spaces", supplied: "req 1"},
		{name: "line break", supplied: "req-1\nlevel=ERROR"},
		{name: "other punctuation", supplied: "req;1"},
		{name: "non-ASCII letters", supplied: "réq-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/teams", http.NoBody)
			if tt.supplied != "" {
				req.Header.Set(middleware.RequestIDHeader, tt.supplied)
			}

			header, inContext := requestIDs(req)

			if tt.accepted {
				assert.Equal(t, tt.supplied, header)
			} else {
				assert.Regexp(t, generatedID, header, "a missing or rejected id is replaced by 16 random bytes in hex")
			}
			assert.Equal(t, header, inContext, "the request context holds the id sent back")
		})
	}
}

func TestRequestID_GeneratesDistinctIDs(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)

	for range 100 {
		id, _ := requestIDs(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
		require.False(t, seen[id], "id %s generated twice", id)
		seen[id] = true
	}
}
