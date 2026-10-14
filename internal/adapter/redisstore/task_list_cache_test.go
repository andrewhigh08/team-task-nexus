package redisstore_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/redisstore"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

const (
	testTTL    = 5 * time.Minute
	testTeamID = int64(7)
)

// testQuery is a normalized query of the test team without filters.
var testQuery = domain.TaskQuery{ViewerID: 1, TeamID: testTeamID, Page: 1, PageSize: 20}

func newTaskListCache(t *testing.T) (*redisstore.TaskListCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return redisstore.NewTaskListCache(client, testTTL), mr
}

// testPage returns a page holding one task with every optional field set and one with none.
func testPage() *domain.TaskPage {
	created := time.Date(2026, 3, 1, 9, 30, 15, 0, time.UTC)
	due := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	completed := created.Add(26 * time.Hour)
	assignee := int64(3)
	return &domain.TaskPage{
		Tasks: []domain.Task{
			{
				ID: 12, TeamID: testTeamID, Title: "Rotate the certificates", Description: "Before they expire",
				Status: domain.TaskStatusDone, Priority: domain.TaskPriorityHigh, CreatorID: 1, AssigneeID: &assignee,
				DueDate: &due, CompletedAt: &completed, CreatedAt: created, UpdatedAt: completed,
			},
			{
				ID: 11, TeamID: testTeamID, Title: "Write the runbook", Status: domain.TaskStatusTodo,
				Priority: domain.TaskPriorityLow, CreatorID: 2, CreatedAt: created, UpdatedAt: created,
			},
		},
		Total: 22, Page: 1, PageSize: 20, TotalPages: 2,
	}
}

func TestTaskListCache_Version(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		invalidations int
		want          int64
	}{
		{name: "never invalidated", invalidations: 0, want: 0},
		{name: "invalidated once", invalidations: 1, want: 1},
		{name: "invalidated three times", invalidations: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache, _ := newTaskListCache(t)
			for range tt.invalidations {
				require.NoError(t, cache.Invalidate(t.Context(), testTeamID))
			}

			got, err := cache.Version(t.Context(), testTeamID)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			other, err := cache.Version(t.Context(), testTeamID+1)
			require.NoError(t, err)
			assert.Zero(t, other, "every team has a version of its own")
		})
	}
}

func TestTaskListCache_Invalidate_KeepsAVersionCounterThatNeverExpires(t *testing.T) {
	t.Parallel()
	cache, mr := newTaskListCache(t)

	require.NoError(t, cache.Invalidate(t.Context(), testTeamID))
	require.NoError(t, cache.Invalidate(t.Context(), testTeamID))

	got, err := mr.Get("tasks:team:7:ver")
	require.NoError(t, err)
	assert.Equal(t, "2", got)
	assert.Zero(t, mr.TTL("tasks:team:7:ver"))
}

func TestTaskListCache_SetAndGet_RoundTripAPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		page *domain.TaskPage
	}{
		{name: "tasks with and without optional fields", page: testPage()},
		{name: "no tasks", page: &domain.TaskPage{Tasks: []domain.Task{}, Page: 1, PageSize: 20}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache, _ := newTaskListCache(t)
			require.NoError(t, cache.Set(t.Context(), testTeamID, 3, testQuery, tt.page))

			got, found, err := cache.Get(t.Context(), testTeamID, 3, testQuery)

			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, tt.page, got)
		})
	}
}

func TestTaskListCache_Set_StoresThePageUnderTheQueryKeyForTheTTL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ver     int64
		q       domain.TaskQuery
		wantKey string
	}{
		{name: "no filters", ver: 0, q: testQuery, wantKey: "tasks:team:7:v0:s=:a=0:p=1:n=20"},
		{
			name:    "every filter",
			ver:     5,
			q:       domain.TaskQuery{ViewerID: 1, TeamID: testTeamID, Status: domain.TaskStatusInProgress, AssigneeID: 3, Page: 2, PageSize: 50},
			wantKey: "tasks:team:7:v5:s=in_progress:a=3:p=2:n=50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache, mr := newTaskListCache(t)

			require.NoError(t, cache.Set(t.Context(), testTeamID, tt.ver, tt.q, testPage()))

			assert.Equal(t, []string{tt.wantKey}, mr.Keys())
			assert.Equal(t, testTTL, mr.TTL(tt.wantKey))
			mr.FastForward(testTTL)
			_, found, err := cache.Get(t.Context(), testTeamID, tt.ver, tt.q)
			require.NoError(t, err)
			assert.False(t, found, "the page expires with the TTL")
		})
	}
}

func TestTaskListCache_Get(t *testing.T) {
	t.Parallel()
	// Every case caches testPage for testQuery under version 0 first.
	tests := []struct {
		name       string
		invalidate bool
		ver        int64
		q          domain.TaskQuery
		wantFound  bool
	}{
		{name: "same query and version", ver: 0, q: testQuery, wantFound: true},
		{
			name: "another viewer of the team shares the page", ver: 0,
			q:         domain.TaskQuery{ViewerID: 2, TeamID: testTeamID, Page: 1, PageSize: 20},
			wantFound: true,
		},
		{name: "after an invalidation", invalidate: true, ver: 1, q: testQuery},
		{name: "another version", ver: 2, q: testQuery},
		{name: "another team", ver: 0, q: domain.TaskQuery{ViewerID: 1, TeamID: testTeamID + 1, Page: 1, PageSize: 20}},
		{name: "another status", ver: 0, q: domain.TaskQuery{ViewerID: 1, TeamID: testTeamID, Status: domain.TaskStatusTodo, Page: 1, PageSize: 20}},
		{name: "another assignee", ver: 0, q: domain.TaskQuery{ViewerID: 1, TeamID: testTeamID, AssigneeID: 3, Page: 1, PageSize: 20}},
		{name: "another page", ver: 0, q: domain.TaskQuery{ViewerID: 1, TeamID: testTeamID, Page: 2, PageSize: 20}},
		{name: "another page size", ver: 0, q: domain.TaskQuery{ViewerID: 1, TeamID: testTeamID, Page: 1, PageSize: 10}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache, _ := newTaskListCache(t)
			require.NoError(t, cache.Set(t.Context(), testTeamID, 0, testQuery, testPage()))
			if tt.invalidate {
				require.NoError(t, cache.Invalidate(t.Context(), testTeamID))
				ver, err := cache.Version(t.Context(), testTeamID)
				require.NoError(t, err)
				require.Equal(t, tt.ver, ver)
			}

			got, found, err := cache.Get(t.Context(), tt.q.TeamID, tt.ver, tt.q)

			require.NoError(t, err)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, testPage(), got)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestTaskListCache_Get_FailsOnACorruptPage(t *testing.T) {
	t.Parallel()
	cache, mr := newTaskListCache(t)
	require.NoError(t, mr.Set("tasks:team:7:v0:s=:a=0:p=1:n=20", `{"tasks":[{"id":"twelve"}]}`))

	got, found, err := cache.Get(t.Context(), testTeamID, 0, testQuery)

	require.ErrorContains(t, err, "decode cached task list tasks:team:7:v0:s=:a=0:p=1:n=20")
	assert.False(t, found)
	assert.Nil(t, got)
}

func TestTaskListCache_Version_FailsOnACorruptCounter(t *testing.T) {
	t.Parallel()
	cache, mr := newTaskListCache(t)
	require.NoError(t, mr.Set("tasks:team:7:ver", "seven"))

	_, err := cache.Version(t.Context(), testTeamID)

	require.ErrorContains(t, err, "read task list version of team 7")
}

func TestTaskListCache_FailsWhenRedisIsDown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		call    func(t *testing.T, cache *redisstore.TaskListCache) error
		wantErr string
	}{
		{
			name: "version",
			call: func(t *testing.T, cache *redisstore.TaskListCache) error {
				_, err := cache.Version(t.Context(), testTeamID)
				return err
			},
			wantErr: "read task list version of team 7",
		},
		{
			name: "get",
			call: func(t *testing.T, cache *redisstore.TaskListCache) error {
				_, _, err := cache.Get(t.Context(), testTeamID, 0, testQuery)
				return err
			},
			wantErr: "read cached task list tasks:team:7:v0:s=:a=0:p=1:n=20",
		},
		{
			name: "set",
			call: func(t *testing.T, cache *redisstore.TaskListCache) error {
				return cache.Set(t.Context(), testTeamID, 0, testQuery, testPage())
			},
			wantErr: "cache task list tasks:team:7:v0:s=:a=0:p=1:n=20",
		},
		{
			name: "invalidate",
			call: func(t *testing.T, cache *redisstore.TaskListCache) error {
				return cache.Invalidate(t.Context(), testTeamID)
			},
			wantErr: "invalidate task lists of team 7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cache, mr := newTaskListCache(t)
			mr.Close()

			require.ErrorContains(t, tt.call(t, cache), tt.wantErr)
		})
	}
}
