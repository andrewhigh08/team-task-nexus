// Package handler implements the HTTP handlers of the API.
package handler

import (
	"net/http"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
)

// HealthHandler serves the health check.
type HealthHandler struct{}

// NewHealthHandler returns a HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health reports that the process is serving requests.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
