package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/mysqlrepo"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

const day = 24 * time.Hour

func TestTeamStatsCountOnlyRecentCompletions(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Analytics")
	member := c.mustJoin(t, owner, team.ID, "Member", "member")
	backlog := c.mustCreateTeam(t, owner, "Backlog")

	c.mustCreateTask(t, owner, team.ID, "Open", nil)
	inReview := c.mustCreateTask(t, owner, team.ID, "In review", nil)
	c.mustPatchTask(t, owner, inReview.ID, map[string]any{"status": "review"})
	c.completeTask(t, owner, team.ID, owner)
	lastWeek := c.completeTask(t, owner, team.ID, owner)
	longAgo := c.completeTask(t, owner, team.ID, owner)
	db := sharedDB(t)
	setCompletedAt(t, db, lastWeek.ID, time.Now().Add(-6*day))
	setCompletedAt(t, db, longAgo.ID, time.Now().Add(-8*day))

	stats := expect[[]statsData](t, c.do(t, http.MethodGet, "/api/v1/teams/stats", nil, owner.Token), http.StatusOK)
	assert.Equal(t, []statsData{
		{TeamID: team.ID, Name: "Analytics", MemberCount: 2, OpenTasks: 2, DoneLast7Days: 2},
		{TeamID: backlog.ID, Name: "Backlog", MemberCount: 1},
	}, stats, "every team of the caller, by name")

	memberStats := expect[[]statsData](t, c.do(t, http.MethodGet, "/api/v1/teams/stats", nil, member.Token), http.StatusOK)
	assert.Equal(t, stats[:1], memberStats)
}

func TestTopContributorsShareRanksOnTies(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Contributors")
	completed := map[string]int{"Alice": 3, "Bob": 3, "Carol": 2, "Erin": 2, "Gina": 1}
	members := make(map[string]account, len(completed))
	for name, n := range completed {
		members[name] = c.mustJoin(t, owner, team.ID, name, "member")
		for range n {
			c.completeTask(t, owner, team.ID, members[name])
		}
	}
	// Completions older than the window do not count, nor do those of former members.
	dave := c.mustJoin(t, owner, team.ID, "Dave", "member")
	setCompletedAt(t, sharedDB(t), c.completeTask(t, owner, team.ID, dave).ID, time.Now().Add(-31*day))
	frank := c.mustJoin(t, owner, team.ID, "Frank", "member")
	for range 4 {
		c.completeTask(t, owner, team.ID, frank)
	}
	requireStatus(t, c.removeMember(t, owner, team.ID, frank.ID), http.StatusNoContent)
	// Nor do open tasks.
	c.mustCreateTask(t, owner, team.ID, "Still open", map[string]any{"assignee_id": members["Gina"].ID})

	got := expect[[]contributorData](t, c.do(t, http.MethodGet, teamPath(team.ID, "/top-contributors"), nil, dave.Token), http.StatusOK)
	contributor := func(name string, tasks, rank int) contributorData {
		return contributorData{UserID: members[name].ID, FullName: name, TasksCompleted: tasks, Rank: rank}
	}
	// Ties share a rank and the next rank is skipped; the top three ranks include everyone
	// tied at the third, while Gina ranks fifth.
	assert.Equal(t, []contributorData{
		contributor("Alice", 3, 1),
		contributor("Bob", 3, 1),
		contributor("Carol", 2, 3),
		contributor("Erin", 2, 3),
	}, got)
}

// TestAnalyticsWindowsIncludeTheirStart checks the repository at the edge of the windows, which
// the API, reading the clock, cannot hit exactly: a completion at since counts, one a second
// earlier does not.
func TestAnalyticsWindowsIncludeTheirStart(t *testing.T) {
	t.Parallel()
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Windows")
	dev := c.mustJoin(t, owner, team.ID, "Developer", "member")
	task := c.completeTask(t, owner, team.ID, dev)
	db := sharedDB(t)
	// completed_at keeps whole seconds.
	completedAt := time.Now().Add(-3 * day).UTC().Truncate(time.Second)
	setCompletedAt(t, db, task.ID, completedAt)
	teams := mysqlrepo.NewTeamRepo(db)

	tests := []struct {
		name    string
		since   time.Time
		counted int
	}{
		{name: "completed at the start of the window", since: completedAt, counted: 1},
		{name: "completed before the window", since: completedAt.Add(time.Second), counted: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stats, err := teams.Stats(t.Context(), dev.ID, tc.since)
			require.NoError(t, err)
			require.Len(t, stats, 1)
			assert.Equal(t, domain.TeamStats{TeamID: team.ID, Name: "Windows", MemberCount: 2, DoneLast7Days: tc.counted}, stats[0])

			contributors, err := teams.TopContributors(t.Context(), team.ID, tc.since, 3)
			require.NoError(t, err)
			assert.Len(t, contributors, tc.counted)
		})
	}
}

// completeTask creates a task in the team assigned to assignee, who then marks it done.
func (c *apiClient) completeTask(t *testing.T, creator account, teamID int64, assignee account) taskData {
	t.Helper()
	task := c.mustCreateTask(t, creator, teamID, "Task for "+assignee.FullName, map[string]any{"assignee_id": assignee.ID})
	done := c.mustPatchTask(t, assignee, task.ID, map[string]any{"status": "done"})
	require.NotNil(t, done.CompletedAt)
	return done
}

// setCompletedAt moves the completion of a done task to at, which the API cannot do: it
// completes tasks at the current time.
func setCompletedAt(t *testing.T, db *sqlx.DB, taskID int64, at time.Time) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), "UPDATE tasks SET completed_at = ? WHERE id = ? AND status = 'done'", at.UTC(), taskID)
	require.NoError(t, err)
}
