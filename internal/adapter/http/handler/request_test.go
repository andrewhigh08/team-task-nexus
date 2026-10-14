package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const testUserID int64 = 42

// Error bodies shared by the handler tests.
const (
	invalidBody   = `{"success":false,"error":{"code":"BAD_REQUEST","message":"invalid request body"}}`
	internalError = `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`
)

// serve routes a request by testUserID through a router that mounts h at pattern, so that
// URL parameters are resolved as in production. A body is sent as JSON.
func serve(t *testing.T, h http.HandlerFunc, method, pattern, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.MethodFunc(method, pattern, h)
	ctx := middleware.WithUserID(t.Context(), testUserID)
	req := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// handlerTest is one request to a handler method and the answer it must get. expect sets up
// the service calls the request makes; nil means it makes none.
type handlerTest[S any] struct {
	name       string
	target     string
	body       string
	expect     func(svc S)
	wantStatus int
	wantBody   string // JSON; empty for an answer without a body
}

// runHandlerTests serves every test with method of a handler built by newHandler, mounted at
// pattern.
func runHandlerTests[S, H any](
	t *testing.T, newHandler func(*testing.T, func(S)) H, method func(H, http.ResponseWriter, *http.Request),
	httpMethod, pattern string, tests []handlerTest[S],
) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHandler(t, tt.expect)
			serveH := func(w http.ResponseWriter, r *http.Request) { method(h, w, r) }

			rec := serve(t, serveH, httpMethod, pattern, tt.target, tt.body)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody == "" {
				assert.Empty(t, rec.Body.String())
				return
			}
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

// invalidFieldBody is the error body of a request field that failed to parse.
func invalidFieldBody(field, problem string) string {
	return `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"invalid input","details":{"fields":{"` +
		field + `":"` + problem + `"}}}}`
}

// notFoundBody is the error body of a missing resource.
func notFoundBody(resource string, id int64) string {
	return `{"success":false,"error":{"code":"NOT_FOUND","message":"` + resource + ` not found",` +
		`"details":{"resource":"` + resource + `","id":` + strconv.FormatInt(id, 10) + `}}}`
}

// forbiddenBody is the error body of a refused operation.
func forbiddenBody(message string) string {
	return `{"success":false,"error":{"code":"FORBIDDEN","message":"` + message + `"}}`
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()
	type body struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name        string
		contentType string
		body        string
		limit       int64 // 0: no body limit
		want        string
		wantCode    string
		wantStatus  int
		wantDetails map[string]any
	}{
		{name: "object", contentType: "application/json", body: `{"name":"Platform"}`, want: "Platform"},
		{name: "charset parameter", contentType: "application/json; charset=utf-8", body: `{"name":"Platform"}`, want: "Platform"},
		{name: "media type in upper case", contentType: "Application/JSON", body: `{"name":"Platform"}`, want: "Platform"},
		{name: "trailing whitespace", contentType: "application/json", body: "{\"name\":\"Platform\"}\n", want: "Platform"},
		{
			name: "unknown field", contentType: "application/json", body: `{"name":"Platform","nickname":"x"}`,
			wantCode: apperror.CodeBadRequest, wantStatus: http.StatusBadRequest,
		},
		{
			name: "second value", contentType: "application/json", body: `{"name":"a"}{"name":"b"}`,
			wantCode: apperror.CodeBadRequest, wantStatus: http.StatusBadRequest,
		},
		{
			name: "trailing garbage", contentType: "application/json", body: `{"name":"a"}]`,
			wantCode: apperror.CodeBadRequest, wantStatus: http.StatusBadRequest,
		},
		{name: "malformed", contentType: "application/json", body: `{"name":`, wantCode: apperror.CodeBadRequest, wantStatus: http.StatusBadRequest},
		{name: "wrong type", contentType: "application/json", body: `{"name":7}`, wantCode: apperror.CodeBadRequest, wantStatus: http.StatusBadRequest},
		{name: "no body", wantCode: apperror.CodeBadRequest, wantStatus: http.StatusBadRequest},
		{
			name: "body without a content type", body: `{"name":"Platform"}`,
			wantCode: apperror.CodeUnsupportedMediaType, wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "body of another media type", contentType: "text/plain", body: `{"name":"Platform"}`,
			wantCode: apperror.CodeUnsupportedMediaType, wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "malformed content type", contentType: "application/json; charset", body: `{"name":"Platform"}`,
			wantCode: apperror.CodeUnsupportedMediaType, wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "body over the limit", contentType: "application/json", body: `{"name":"Platform"}`, limit: 10,
			wantCode: apperror.CodePayloadTooLarge, wantStatus: http.StatusRequestEntityTooLarge,
			wantDetails: map[string]any{"limit_bytes": float64(10)},
		},
		{
			name: "trailing data over the limit", contentType: "application/json", body: `{"name":"a"}` + strings.Repeat(" ", 20), limit: 20,
			wantCode: apperror.CodePayloadTooLarge, wantStatus: http.StatusRequestEntityTooLarge,
			wantDetails: map[string]any{"limit_bytes": float64(20)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			if tt.limit > 0 {
				req.Body = http.MaxBytesReader(rec, req.Body, tt.limit)
			}
			var got body

			ok := decodeJSON(rec, req, &got)

			if tt.wantCode == "" {
				require.True(t, ok, "body: %s", rec.Body.String())
				assert.Equal(t, tt.want, got.Name)
				assert.Zero(t, rec.Body.Len(), "nothing is written for the handler to overwrite")
				return
			}
			require.False(t, ok)
			assert.Equal(t, tt.wantStatus, rec.Code)
			env := decodeError(t, rec)
			assert.Equal(t, tt.wantCode, env.Code)
			assert.Equal(t, tt.wantDetails, env.Details)
		})
	}
}

// errorBody is the error object of an error envelope.
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var env struct {
		Success bool      `json:"success"`
		Error   errorBody `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "body: %s", rec.Body.String())
	assert.False(t, env.Success)
	return env.Error
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
