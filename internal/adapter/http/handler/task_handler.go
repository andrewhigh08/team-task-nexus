package handler

import (
	"net/http"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// TaskHandler serves tasks, their history and the orphaned assignments of a team.
type TaskHandler struct {
	tasks port.TaskService
}

// NewTaskHandler returns a TaskHandler backed by tasks.
func NewTaskHandler(tasks port.TaskService) *TaskHandler {
	return &TaskHandler{tasks: tasks}
}

// Create adds a task to a team of the caller and answers 201 with it.
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req createTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := req.toInput()
	if err != nil {
		response.Error(w, r, err)
		return
	}

	task, err := h.tasks.Create(r.Context(), userID, in)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, newTaskResponse(*task))
}

// List returns a page of the tasks of the caller's teams, narrowed by the query parameters
// team_id, status and assignee_id and paged by page and page_size.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	q, err := taskQuery(r, userID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	page, err := h.tasks.List(r.Context(), q)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSONWithMeta(w, mapAll(page.Tasks, newTaskResponse), response.Meta{
		Page:       page.Page,
		PageSize:   page.PageSize,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	})
}

// taskQuery reads the task list query parameters. Absent ones are zero, which the service
// treats as "any" or as the default; malformed ones are rejected rather than ignored.
func taskQuery(r *http.Request, viewerID int64) (domain.TaskQuery, error) {
	q := domain.TaskQuery{ViewerID: viewerID, Status: domain.TaskStatus(r.URL.Query().Get("status"))}
	var err error
	if q.TeamID, err = queryID(r, "team_id"); err != nil {
		return domain.TaskQuery{}, err
	}
	if q.AssigneeID, err = queryID(r, "assignee_id"); err != nil {
		return domain.TaskQuery{}, err
	}
	page, err := pageQuery(r)
	if err != nil {
		return domain.TaskQuery{}, err
	}
	q.Page, q.PageSize = page.Page, page.PageSize
	return q, nil
}

// Get returns the task in the path.
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	task, err := h.tasks.Get(r.Context(), userID, taskID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, newTaskResponse(*task))
}

// Update applies a partial update to the task in the path and returns the result.
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req updateTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := req.toInput()
	if err != nil {
		response.Error(w, r, err)
		return
	}

	task, err := h.tasks.Update(r.Context(), userID, taskID, in)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, newTaskResponse(*task))
}

// History returns a page of the change history of the task in the path, newest first, paged
// by the query parameters page and page_size.
func (h *TaskHandler) History(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	p, err := pageQuery(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	page, err := h.tasks.History(r.Context(), userID, taskID, p)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSONWithMeta(w, mapAll(page.Changes, newTaskChangeResponse), response.Meta{
		Page:       page.Page,
		PageSize:   page.PageSize,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	})
}

// ListOrphaned returns the tasks of the team in the path that are assigned to former members.
func (h *TaskHandler) ListOrphaned(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	teamID, err := pathID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	orphaned, err := h.tasks.ListOrphaned(r.Context(), userID, teamID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAll(orphaned, newOrphanedAssignmentResponse))
}
