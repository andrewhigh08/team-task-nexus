package handler

import (
	"net/http"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

type createCommentRequest struct {
	Content string `json:"content"`
}

type commentResponse struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newCommentResponse(c domain.Comment) commentResponse {
	return commentResponse{
		ID:        c.ID,
		TaskID:    c.TaskID,
		UserID:    c.UserID,
		Content:   c.Content,
		CreatedAt: c.CreatedAt.UTC(),
		UpdatedAt: c.UpdatedAt.UTC(),
	}
}

// CommentHandler serves task comments.
type CommentHandler struct {
	comments port.CommentService
}

// NewCommentHandler returns a CommentHandler backed by comments.
func NewCommentHandler(comments port.CommentService) *CommentHandler {
	return &CommentHandler{comments: comments}
}

// Create adds a comment to the task in the path and answers 201 with it.
func (h *CommentHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req createCommentRequest
	if err = decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	comment, err := h.comments.Create(r.Context(), userID, taskID, port.CreateCommentInput{Content: req.Content})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, newCommentResponse(*comment))
}

// List returns the comments of the task in the path, oldest first.
func (h *CommentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	comments, err := h.comments.List(r.Context(), userID, taskID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAll(comments, newCommentResponse))
}
