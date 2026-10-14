package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenAPI(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\n"
	rec := httptest.NewRecorder()

	OpenAPI(spec)(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, spec, rec.Body.String())
}
