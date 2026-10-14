package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// TaskHandler serves tasks and their history.
type TaskHandler struct {
	taskSvc port.TaskService
}

// NewTaskHandler returns a TaskHandler backed by taskSvc.
func NewTaskHandler(taskSvc port.TaskService) *TaskHandler {
	return &TaskHandler{taskSvc: taskSvc}
}

// Create adds a task to a team.
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	var req domain.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, apperror.BadRequest("invalid request body"))
		return
	}

	task, err := h.taskSvc.Create(r.Context(), userID, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusCreated, task)
}

// List returns one page of tasks matching the query parameters.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	filter := domain.TaskFilter{
		Page:     1,
		PageSize: 20,
	}

	if v := r.URL.Query().Get("team_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.TeamID = id
		}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		filter.Status = v
	}
	if v := r.URL.Query().Get("assignee_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.AssigneeID = id
		}
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			filter.Page = p
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil {
			filter.PageSize = ps
		}
	}

	result, err := h.taskSvc.List(r.Context(), userID, filter)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, result)
}

// Update changes the task in the path.
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, r, apperror.BadRequest("invalid task id"))
		return
	}

	var req domain.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, apperror.BadRequest("invalid request body"))
		return
	}

	task, err := h.taskSvc.Update(r.Context(), userID, taskID, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, task)
}

// GetHistory returns the change history of the task in the path.
func (h *TaskHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, r, apperror.BadRequest("invalid task id"))
		return
	}

	history, err := h.taskSvc.GetHistory(r.Context(), userID, taskID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, history)
}

// GetOrphanedAssignees returns the tasks assigned to users outside the task's team.
func (h *TaskHandler) GetOrphanedAssignees(w http.ResponseWriter, r *http.Request) {
	result, err := h.taskSvc.GetOrphanedAssignees(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, result)
}
