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

// CommentHandler serves task comments.
type CommentHandler struct {
	commentSvc port.CommentService
}

// NewCommentHandler returns a CommentHandler backed by commentSvc.
func NewCommentHandler(commentSvc port.CommentService) *CommentHandler {
	return &CommentHandler{commentSvc: commentSvc}
}

// Create adds a comment to the task in the path.
func (h *CommentHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, r, apperror.BadRequest("invalid task id"))
		return
	}

	var req domain.CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, apperror.BadRequest("invalid request body"))
		return
	}

	comment, err := h.commentSvc.Create(r.Context(), userID, taskID, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusCreated, comment)
}

// List returns the comments of the task in the path.
func (h *CommentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, r, apperror.BadRequest("invalid task id"))
		return
	}

	comments, err := h.commentSvc.ListByTaskID(r.Context(), userID, taskID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, comments)
}
