package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

const bearerScheme = "Bearer"

// userIDKey is the context key of the authenticated user id. The type is unexported, so no
// other package can set or overwrite the value.
type userIDKey struct{}

// Authenticate admits requests that carry a bearer token accepted by verifier and stores the
// token's user id in the request context. Every other request is answered with 401.
func Authenticate(verifier port.TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Values("Authorization")
			if len(header) == 0 {
				unauthorized(w, r, "")
				return
			}
			token, ok := bearerToken(header)
			if !ok {
				unauthorized(w, r, "invalid authorization header")
				return
			}
			userID, err := verifier.Verify(token)
			if err != nil {
				logger.FromContext(r.Context()).Debug("bearer token rejected", "error", err)
				unauthorized(w, r, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
		})
	}
}

// bearerToken extracts the token from the Authorization header values of a request. Only a
// single header holding the scheme and exactly one token is accepted; the scheme is
// case-insensitive (RFC 9110).
func bearerToken(header []string) (string, bool) {
	if len(header) != 1 {
		return "", false
	}
	fields := strings.Fields(header[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], bearerScheme) {
		return "", false
	}
	return fields[1], true
}

// unauthorized answers 401 with the challenge RFC 9110 requires on that status; an empty
// message means "authentication required".
func unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("WWW-Authenticate", bearerScheme)
	response.Error(w, r, apperror.Unauthorized(message))
}

// WithUserID returns a copy of ctx carrying the authenticated user id, where UserIDFrom and
// the request logger find it, and reports the user to the access log of the request.
// Authenticate calls it for every admitted request; tests use it to stand in for the
// middleware.
func WithUserID(ctx context.Context, userID int64) context.Context {
	recordUser(ctx, userID)
	ctx = context.WithValue(ctx, userIDKey{}, userID)
	return logger.WithUserIDContext(ctx, userID)
}

// UserIDFrom returns the user id stored by Authenticate and whether there is one.
func UserIDFrom(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey{}).(int64)
	return userID, ok
}
