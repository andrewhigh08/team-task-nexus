package middleware

import "net/http"

// BodyLimit caps request bodies at limit bytes. Reading beyond the limit fails with
// *http.MaxBytesError, which the handlers answer with 413.
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
