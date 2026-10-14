package middleware

import (
	"net/http"
	"strings"
)

// SecurityHeaders sets the headers that harden every response: browsers must not guess
// content types, frame the responses or send referrers. Responses of the API must also not be
// stored by any cache, since they hold the data of one user.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
