package handler

import (
	"encoding/json"
	"net/http"

	"github.com/shalfey088/team-task-nexus/internal/adapter/http/response"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type AuthHandler struct {
	authSvc port.AuthService
}

func NewAuthHandler(authSvc port.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req domain.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body"))
		return
	}

	result, err := h.authSvc.Register(r.Context(), req)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, result)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req domain.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body"))
		return
	}

	result, err := h.authSvc.Login(r.Context(), req)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, result)
}
