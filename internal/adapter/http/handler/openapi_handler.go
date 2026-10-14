package handler

import (
	"io"
	"net/http"
)

// OpenAPI returns a handler that serves spec, the OpenAPI document of the API in YAML.
func OpenAPI(spec string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		// A failed write means the client has gone; there is no one left to tell.
		_, _ = io.WriteString(w, spec)
	}
}
