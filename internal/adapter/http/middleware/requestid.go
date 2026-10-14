package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// RequestIDHeader carries the request id in requests and responses.
const RequestIDHeader = "X-Request-ID"

// requestIDBytes is the size of a generated request id: 32 hex characters.
const requestIDBytes = 16

// requestIDPattern admits the ids a client may choose: short, and without characters that
// could break a log line or a header.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID assigns every request an id: the client's X-Request-ID when it is 1 to 64
// characters of [A-Za-z0-9._-], a new random one otherwise. The id is sent back in the
// X-Request-ID response header and stored in the request context, where the logger finds it,
// so a client can quote it to find the log records of its request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(logger.WithRequestIDContext(r.Context(), id)))
	})
}

func newRequestID() string {
	buf := make([]byte, requestIDBytes)
	// crypto/rand.Read never returns an error; it crashes the program instead.
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
