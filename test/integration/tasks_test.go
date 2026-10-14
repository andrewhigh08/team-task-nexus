package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// change is the part of a history entry a test predicts.
type change struct {
	UserID   int64
	Field    string
	OldValue string
	NewValue string
}

func changesOf(history []changeData) []change {
	out := make([]change, 0, len(history))
	for _, h := range history {
		out = append(out, change{UserID: h.UserID, Field: h.Field, OldValue: h.OldValue, NewValue: h.NewValue})
	}
	return out
}

func TestTaskUpdatesAreRecordedInHistory(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Board")
	dev := c.mustJoin(t, owner, team.ID, "Developer", "member")

	created := c.mustCreateTask(t, owner, team.ID, "  Write docs  ", map[string]any{
		"description": "First draft", "priority": 3, "assignee_id": dev.ID, "due_date": "2026-12-31",
	})
	assert.Equal(t, "Write docs", created.Title)
	assert.Equal(t, "todo", created.Status)
	assert.Equal(t, 3, created.Priority)
	assert.Equal(t, owner.ID, created.CreatorID)
	assert.Equal(t, &dev.ID, created.AssigneeID)
	assert.Equal(t, new("2026-12-31"), created.DueDate)
	assert.Nil(t, created.CompletedAt)
	assert.Empty(t, c.history(t, owner, created.ID), "creating a task is not a change")

	done := c.mustPatchTask(t, dev, created.ID, map[string]any{"status": "done", "title": "Write the docs"})
	assert.Equal(t, "done", done.Status)
	assert.Equal(t, "Write the docs", done.Title)
	require.NotNil(t, done.CompletedAt)
	assert.WithinDuration(t, time.Now(), *done.CompletedAt, time.Minute)
	assert.Equal(t, created.Priority, done.Priority, "fields absent from the patch are kept")
	assert.Equal(t, created.AssigneeID, done.AssigneeID)
	assert.Equal(t, created.DueDate, done.DueDate)

	// Leaving done clears the completion; null clears the assignee and the due date.
	reopened := c.mustPatchTask(t, owner, created.ID, map[string]any{"status": "in_progress", "assignee_id": nil, "due_date": nil})
	assert.Equal(t, "in_progress", reopened.Status)
	assert.Nil(t, reopened.CompletedAt)
	assert.Nil(t, reopened.AssigneeID)
	assert.Nil(t, reopened.DueDate)
	assert.Equal(t, "Write the docs", reopened.Title)

	// Values equal to the current ones change nothing and are not recorded.
	unchanged := c.mustPatchTask(t, owner, created.ID, map[string]any{"status": "in_progress", "title": " Write the docs "})
	assert.Equal(t, reopened, unchanged)
	assert.Equal(t, reopened, c.mustGetTask(t, dev, created.ID))

	history := c.history(t, dev, created.ID)
	assert.Equal(t, []change{
		{UserID: owner.ID, Field: "due_date", OldValue: "2026-12-31", NewValue: ""},
		{UserID: owner.ID, Field: "assignee_id", OldValue: strconv.FormatInt(dev.ID, 10), NewValue: ""},
		{UserID: owner.ID, Field: "status", OldValue: "done", NewValue: "in_progress"},
		{UserID: dev.ID, Field: "status", OldValue: "todo", NewValue: "done"},
		{UserID: dev.ID, Field: "title", OldValue: "Write docs", NewValue: "Write the docs"},
	}, changesOf(history), "newest first")
	for _, h := range history {
		assert.Equal(t, created.ID, h.TaskID)
		assert.WithinDuration(t, time.Now(), h.ChangedAt, time.Minute)
	}
}

func TestTaskInputIsValidated(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Validation")
	outsider := c.mustRegister(t, "Outsider")
	task := c.mustCreateTask(t, owner, team.ID, "Valid task", nil)

	patches := []struct {
		name    string
		body    any
		field   string
		problem string
	}{
		{name: "unknown status", body: map[string]any{"status": "archived"}, field: "status", problem: "must be one of todo, in_progress, review, done"},
		{name: "null status", body: map[string]any{"status": nil}, field: "status", problem: "must not be null"},
		{name: "priority out of range", body: map[string]any{"priority": 5}, field: "priority", problem: "must be 1 (low), 2 (medium) or 3 (high)"},
		{name: "assignee outside the team", body: map[string]any{"assignee_id": outsider.ID}, field: "assignee_id", problem: "must be a member of the team"},
		{name: "assignee id zero", body: map[string]any{"assignee_id": 0}, field: "assignee_id", problem: "must be a positive integer"},
		{name: "blank title", body: map[string]any{"title": "   "}, field: "title", problem: "is required"},
		{name: "null title", body: map[string]any{"title": nil}, field: "title", problem: "must not be null"},
		{name: "malformed due date", body: map[string]any{"due_date": "31.12.2026"}, field: "due_date", problem: "must be a date in YYYY-MM-DD format"},
	}
	for _, tc := range patches {
		t.Run("patch "+tc.name, func(t *testing.T) {
			errBody := requireError(t, c.patchTask(t, owner, task.ID, tc.body), http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Equal(t, tc.problem, fieldProblem(errBody, tc.field))
		})
	}
	requireError(t, c.patchTask(t, owner, task.ID, map[string]any{"creator_id": outsider.ID}), http.StatusBadRequest, "BAD_REQUEST")
	assert.Equal(t, task, c.mustGetTask(t, owner, task.ID), "no rejected patch changed the task")
	assert.Empty(t, c.history(t, owner, task.ID))

	creates := []struct {
		name    string
		body    map[string]any
		field   string
		problem string
	}{
		{name: "missing team", body: map[string]any{"title": "No team"}, field: "team_id", problem: "must be a positive integer"},
		{name: "missing title", body: map[string]any{"team_id": team.ID}, field: "title", problem: "is required"},
		{name: "assignee outside the team", body: map[string]any{"team_id": team.ID, "title": "Assigned", "assignee_id": outsider.ID}, field: "assignee_id", problem: "must be a member of the team"},
		{name: "priority out of range", body: map[string]any{"team_id": team.ID, "title": "Urgent", "priority": 4}, field: "priority", problem: "must be 1 (low), 2 (medium) or 3 (high)"},
	}
	for _, tc := range creates {
		t.Run("create "+tc.name, func(t *testing.T) {
			errBody := requireError(t, c.do(t, http.MethodPost, "/api/v1/tasks", tc.body, owner.Token), http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Equal(t, tc.problem, fieldProblem(errBody, tc.field))
		})
	}
	assert.Equal(t, []int64{task.ID}, taskIDs(t, c, owner, "/api/v1/tasks"), "no rejected task was created")
}

func TestTaskListsFilterPageAndRejectBadQueries(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Lists")
	dev := c.mustJoin(t, owner, team.ID, "Developer", "member")
	first := c.mustCreateTask(t, owner, team.ID, "First", map[string]any{"assignee_id": dev.ID})
	second := c.mustCreateTask(t, owner, team.ID, "Second", nil)
	third := c.mustCreateTask(t, owner, team.ID, "Third", map[string]any{"assignee_id": dev.ID})
	c.mustPatchTask(t, dev, third.ID, map[string]any{"status": "review"})
	teamQuery := "/api/v1/tasks?team_id=" + strconv.FormatInt(team.ID, 10)

	page := c.do(t, http.MethodGet, teamQuery+"&page_size=2", nil, dev.Token)
	assert.Equal(t, []int64{third.ID, second.ID}, idsOf(expect[[]taskData](t, page, http.StatusOK)), "newest first")
	assert.Equal(t, &metaBody{Page: 1, PageSize: 2, Total: 3, TotalPages: 2}, page.Meta)
	last := c.do(t, http.MethodGet, teamQuery+"&page_size=2&page=2", nil, dev.Token)
	assert.Equal(t, []int64{first.ID}, idsOf(expect[[]taskData](t, last, http.StatusOK)))
	assert.Equal(t, &metaBody{Page: 2, PageSize: 2, Total: 3, TotalPages: 2}, last.Meta)
	beyond := c.do(t, http.MethodGet, teamQuery+"&page=3", nil, dev.Token)
	assert.JSONEq(t, `[]`, string(beyond.Data))

	devID := strconv.FormatInt(dev.ID, 10)
	assert.Equal(t, []int64{third.ID, first.ID}, taskIDs(t, c, dev, teamQuery+"&assignee_id="+devID))
	assert.Equal(t, []int64{third.ID}, taskIDs(t, c, dev, teamQuery+"&status=review&assignee_id="+devID))
	assert.Equal(t, []int64{second.ID, first.ID}, taskIDs(t, c, dev, "/api/v1/tasks?status=todo"))

	bad := []struct {
		query string
		field string
	}{
		{query: "?team_id=abc", field: "team_id"},
		{query: "?assignee_id=-1", field: "assignee_id"},
		{query: "?status=archived", field: "status"},
		{query: "?page=0", field: "page"},
		{query: "?page_size=101", field: "page_size"},
		{query: "?page_size=x", field: "page_size"},
	}
	for _, tc := range bad {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			errBody := requireError(t, c.do(t, http.MethodGet, "/api/v1/tasks"+tc.query, nil, dev.Token), http.StatusBadRequest, "VALIDATION_ERROR")
			assert.NotEmpty(t, fieldProblem(errBody, tc.field))
		})
	}
	for _, path := range []string{"/api/v1/tasks/abc", "/api/v1/tasks/0", "/api/v1/teams/-5/members"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			errBody := requireError(t, c.do(t, http.MethodGet, path, nil, dev.Token), http.StatusBadRequest, "VALIDATION_ERROR")
			assert.Equal(t, "must be a positive integer", fieldProblem(errBody, "id"))
		})
	}
}

// TestConcurrentUpdatesAreNotLost has two clients patch different fields of one task at the
// same time. Each update locks the task's row, so none can write back a stale copy of a field
// the other one has just changed, and each history entry records the value it replaced.
func TestConcurrentUpdatesAreNotLost(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Concurrency")
	task := c.mustCreateTask(t, owner, team.ID, "title-0", map[string]any{"description": "description-0"})

	const updates = 10
	fields := []string{"title", "description"}
	errs := make([]error, len(fields))
	var wg sync.WaitGroup
	for i, field := range fields {
		wg.Go(func() { errs[i] = patchRepeatedly(t.Context(), c, owner.Token, task.ID, field, updates) })
	}
	wg.Wait()
	require.NoError(t, errors.Join(errs...))

	final := c.mustGetTask(t, owner, task.ID)
	assert.Equal(t, fmt.Sprintf("title-%d", updates), final.Title)
	assert.Equal(t, fmt.Sprintf("description-%d", updates), final.Description)

	history := c.history(t, owner, task.ID)
	require.Len(t, history, len(fields)*updates)
	slices.Reverse(history)
	for _, field := range fields {
		previous := field + "-0"
		for _, h := range history {
			if h.Field != field {
				continue
			}
			assert.Equalf(t, previous, h.OldValue, "the change to %q replaced the value written before it", h.NewValue)
			previous = h.NewValue
		}
		assert.Equal(t, fmt.Sprintf("%s-%d", field, updates), previous)
	}
}

// patchRepeatedly sets field of the task to field-1 up to field-n, one request after the
// other. It reports failures as errors, since it runs outside the test's goroutine.
func patchRepeatedly(ctx context.Context, c *apiClient, token string, taskID int64, field string, n int) error {
	for i := range n {
		body := map[string]string{field: fmt.Sprintf("%s-%d", field, i+1)}
		req, err := c.buildRequest(ctx, http.MethodPatch, taskPath(taskID, ""), body, token)
		if err != nil {
			return err
		}
		resp, err := c.exchange(req)
		if err != nil {
			return err
		}
		if resp.Status != http.StatusOK {
			return fmt.Errorf("patch %s: status %d: %s", body[field], resp.Status, resp.Body)
		}
	}
	return nil
}

func TestTeamTaskListsAreCachedUntilTheTeamChanges(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Cache")
	task := c.mustCreateTask(t, owner, team.ID, "Original title", nil)
	teamQuery := "/api/v1/tasks?team_id=" + strconv.FormatInt(team.ID, 10)

	listed := expect[[]taskData](t, c.do(t, http.MethodGet, teamQuery, nil, owner.Token), http.StatusOK)
	require.Len(t, listed, 1)
	assert.Equal(t, "Original title", listed[0].Title)

	// A write behind the API's back stays invisible while the team's list is cached...
	_, err := sharedDB(t).ExecContext(t.Context(), "UPDATE tasks SET title = 'Changed in the database' WHERE id = ?", task.ID)
	require.NoError(t, err)
	assert.Equal(t, listed, expect[[]taskData](t, c.do(t, http.MethodGet, teamQuery, nil, owner.Token), http.StatusOK))
	// ...lists across teams, which are never cached, show it at once...
	acrossTeams := expect[[]taskData](t, c.do(t, http.MethodGet, "/api/v1/tasks", nil, owner.Token), http.StatusOK)
	require.Len(t, acrossTeams, 1)
	assert.Equal(t, "Changed in the database", acrossTeams[0].Title)

	// ...and a change through the API retires the cached lists of the team.
	c.mustPatchTask(t, owner, task.ID, map[string]any{"status": "review"})
	listed = expect[[]taskData](t, c.do(t, http.MethodGet, teamQuery, nil, owner.Token), http.StatusOK)
	require.Len(t, listed, 1)
	assert.Equal(t, "Changed in the database", listed[0].Title)
	assert.Equal(t, "review", listed[0].Status)
}
