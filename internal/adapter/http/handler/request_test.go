package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const testUserID int64 = 42

// serve routes a request by testUserID through a router that mounts h at pattern, so that
// URL parameters are resolved as in production.
func serve(t *testing.T, h http.HandlerFunc, method, pattern, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.MethodFunc(method, pattern, h)
	ctx := middleware.WithUserID(t.Context(), testUserID)
	req := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// invalidFieldBody is the error body of a request field that failed to parse.
func invalidFieldBody(field, problem string) string {
	return `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"invalid input","details":{"fields":{"` +
		field + `":"` + problem + `"}}}}`
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()
	type body struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "object", body: `{"name":"Platform"}`, want: "Platform"},
		{name: "trailing whitespace", body: "{\"name\":\"Platform\"}\n", want: "Platform"},
		{name: "unknown field", body: `{"name":"Platform","nickname":"x"}`, wantErr: true},
		{name: "second value", body: `{"name":"a"}{"name":"b"}`, wantErr: true},
		{name: "malformed", body: `{"name":`, wantErr: true},
		{name: "empty", body: ``, wantErr: true},
		{name: "wrong type", body: `{"name":7}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			var got body

			err := decodeJSON(req, &got)

			if tt.wantErr {
				appErr, ok := apperror.AsAppError(err)
				require.True(t, ok, "%v is not an AppError", err)
				assert.Equal(t, apperror.CodeBadRequest, appErr.Code)
				assert.Equal(t, "invalid request body", appErr.Message)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestPathID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		want    int64
		wantErr bool
	}{
		{path: "/tasks/7", want: 7},
		{path: "/tasks/9223372036854775807", want: 9223372036854775807},
		{path: "/tasks/0", wantErr: true},
		{path: "/tasks/-3", wantErr: true},
		{path: "/tasks/abc", wantErr: true},
		{path: "/tasks/9223372036854775808", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			var (
				got int64
				err error
			)
			h := func(_ http.ResponseWriter, r *http.Request) { got, err = pathID(r, "id") }

			serve(t, h, http.MethodGet, "/tasks/{id}", tt.path, "")

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, map[string]any{"fields": map[string]string{"id": msgPositive}}, apperror.FromError(err).Details)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQueryParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query   string
		want    int
		wantErr bool
	}{
		{query: "", want: 0},
		{query: "?page=", want: 0},
		{query: "?page=3", want: 3},
		{query: "?page=0", wantErr: true},
		{query: "?page=-1", wantErr: true},
		{query: "?page=two", wantErr: true},
		{query: "?page=1.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/tasks"+tt.query, http.NoBody)

			n, intErr := queryInt(req, "page")
			id, idErr := queryID(req, "page")

			if tt.wantErr {
				require.Error(t, intErr)
				require.Error(t, idErr)
				return
			}
			require.NoError(t, intErr)
			require.NoError(t, idErr)
			assert.Equal(t, tt.want, n)
			assert.Equal(t, int64(tt.want), id)
		})
	}
}
