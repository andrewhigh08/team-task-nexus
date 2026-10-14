// Package middleware provides the HTTP middleware of the API: authentication, rate limiting,
// request logging and metrics.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

type ctxKey string

// UserIDKey is the context key of the authenticated user id.
const UserIDKey ctxKey = "user_id"

// JWTAuth admits requests carrying a valid bearer token signed with secret and stores the
// token's user id in the request context.
func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(w, r, apperror.Unauthorized(""))
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				response.Error(w, r, apperror.Unauthorized(""))
				return
			}

			token, err := jwt.Parse(parts[1], func(token *jwt.Token) (any, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				response.Error(w, r, apperror.Unauthorized(""))
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				response.Error(w, r, apperror.Unauthorized(""))
				return
			}

			userIDFloat, ok := claims["user_id"].(float64)
			if !ok {
				response.Error(w, r, apperror.Unauthorized(""))
				return
			}

			userID := int64(userIDFloat)
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = logger.WithUserIDContext(ctx, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID returns the user id stored by JWTAuth, or 0 when there is none.
func GetUserID(ctx context.Context) int64 {
	if id, ok := ctx.Value(UserIDKey).(int64); ok {
		return id
	}
	return 0
}
