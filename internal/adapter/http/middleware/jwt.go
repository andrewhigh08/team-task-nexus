package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/shalfey088/team-task-nexus/internal/adapter/http/response"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
)

type ctxKey string

const UserIDKey ctxKey = "user_id"

func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(w, apperror.ErrUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				response.Error(w, apperror.ErrUnauthorized)
				return
			}

			token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, apperror.ErrUnauthorized
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				response.Error(w, apperror.ErrUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				response.Error(w, apperror.ErrUnauthorized)
				return
			}

			userIDFloat, ok := claims["user_id"].(float64)
			if !ok {
				response.Error(w, apperror.ErrUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, int64(userIDFloat))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserID(ctx context.Context) int64 {
	if id, ok := ctx.Value(UserIDKey).(int64); ok {
		return id
	}
	return 0
}
