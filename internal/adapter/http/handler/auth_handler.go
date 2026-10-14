package handler

import (
	"encoding/json"
	"net/http"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// AuthHandler serves registration and login.
type AuthHandler struct {
	authSvc port.AuthService
}

// NewAuthHandler returns an AuthHandler backed by authSvc.
func NewAuthHandler(authSvc port.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

// Register creates an account and answers with a token for it.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req domain.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, apperror.BadRequest("invalid request body"))
		return
	}

	result, err := h.authSvc.Register(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusCreated, result)
}

// Login answers with a token for valid credentials.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req domain.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, apperror.BadRequest("invalid request body"))
		return
	}

	result, err := h.authSvc.Login(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, result)
}
