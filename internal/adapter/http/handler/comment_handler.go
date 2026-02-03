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

type CommentHandler struct {
	commentSvc port.CommentService
}

func NewCommentHandler(commentSvc port.CommentService) *CommentHandler {
	return &CommentHandler{commentSvc: commentSvc}
}

func (h *CommentHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, apperror.BadRequest("invalid task id"))
		return
	}

	var req domain.CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body"))
		return
	}

	comment, err := h.commentSvc.Create(r.Context(), userID, taskID, req)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, comment)
}

func (h *CommentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, apperror.BadRequest("invalid task id"))
		return
	}

	comments, err := h.commentSvc.ListByTaskID(r.Context(), userID, taskID)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, comments)
}
