package handler

import (
	"net/http"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// requireUserID returns the id of the authenticated caller. When there is none, which only
// happens if a route is mounted outside the authentication middleware, it answers 401 and
// reports false.
func requireUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := middleware.UserIDFrom(r.Context())
	if !ok {
		response.Error(w, r, apperror.Unauthorized(""))
	}
	return userID, ok
}
