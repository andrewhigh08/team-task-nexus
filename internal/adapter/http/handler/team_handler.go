package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/shalfey088/team-task-nexus/internal/adapter/http/middleware"
	"github.com/shalfey088/team-task-nexus/internal/adapter/http/response"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type TeamHandler struct {
	teamSvc port.TeamService
}

func NewTeamHandler(teamSvc port.TeamService) *TeamHandler {
	return &TeamHandler{teamSvc: teamSvc}
}

func (h *TeamHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	var req domain.CreateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body"))
		return
	}

	team, err := h.teamSvc.Create(r.Context(), userID, req)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, team)
}

func (h *TeamHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	teams, err := h.teamSvc.ListByUserID(r.Context(), userID)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, teams)
}

func (h *TeamHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, apperror.BadRequest("invalid team id"))
		return
	}

	team, err := h.teamSvc.GetByID(r.Context(), userID, teamID)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, team)
}

func (h *TeamHandler) Invite(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, apperror.BadRequest("invalid team id"))
		return
	}

	var req domain.InviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body"))
		return
	}

	if err := h.teamSvc.InviteUser(r.Context(), userID, teamID, req); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "user invited successfully"})
}

func (h *TeamHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	stats, err := h.teamSvc.GetStats(r.Context(), userID)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, stats)
}

func (h *TeamHandler) GetTopContributors(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, apperror.BadRequest("invalid team id"))
		return
	}

	contributors, err := h.teamSvc.GetTopContributors(r.Context(), userID, teamID)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, contributors)
}
