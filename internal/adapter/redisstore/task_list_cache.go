package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// TaskListCache implements port.TaskListCache. Its keys are
//
//	tasks:team:<id>:ver                                               version counter, no TTL
//	tasks:team:<id>:v<ver>:s=<status>:a=<assignee>:p=<page>:n=<size>  JSON page, TTL as configured
//
// where an empty status and assignee 0 stand for no filter. Invalidation is a single INCR of
// the counter: the pages of older versions are never read again and expire on their own, so no
// key has to be searched for. The counter itself must not expire, since a counter starting over
// from zero could come back to a version whose pages are still cached.
type TaskListCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewTaskListCache returns a TaskListCache on client that keeps every page for ttl.
func NewTaskListCache(client redis.Cmdable, ttl time.Duration) *TaskListCache {
	return &TaskListCache{client: client, ttl: ttl}
}

// Version returns the current version of the team's task list; zero while it has never been
// invalidated.
func (c *TaskListCache) Version(ctx context.Context, teamID int64) (int64, error) {
	ver, err := c.client.Get(ctx, versionKey(teamID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read task list version of team %d: %w", teamID, err)
	}
	return ver, nil
}

// Get returns the page of q cached under version ver of the team's task list and whether
// there was one.
func (c *TaskListCache) Get(ctx context.Context, teamID, ver int64, q domain.TaskQuery) (*domain.TaskPage, bool, error) {
	key := pageKey(teamID, ver, q)
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cached task list %s: %w", key, err)
	}
	var cached cachedPage
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false, fmt.Errorf("decode cached task list %s: %w", key, err)
	}
	return cached.toDomain(), true, nil
}

// Set caches page as the result of q under version ver of the team's task list.
func (c *TaskListCache) Set(ctx context.Context, teamID, ver int64, q domain.TaskQuery, page *domain.TaskPage) error {
	key := pageKey(teamID, ver, q)
	data, err := json.Marshal(newCachedPage(page))
	if err != nil {
		return fmt.Errorf("encode task list %s: %w", key, err)
	}
	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		return fmt.Errorf("cache task list %s: %w", key, err)
	}
	return nil
}

// Invalidate moves the team's task list to its next version.
func (c *TaskListCache) Invalidate(ctx context.Context, teamID int64) error {
	if err := c.client.Incr(ctx, versionKey(teamID)).Err(); err != nil {
		return fmt.Errorf("invalidate task lists of team %d: %w", teamID, err)
	}
	return nil
}

func versionKey(teamID int64) string {
	return "tasks:team:" + strconv.FormatInt(teamID, 10) + ":ver"
}

// pageKey identifies the page q selects within version ver of the team's task list. The viewer
// is not part of it: every member of a team sees the same tasks of that team.
func pageKey(teamID, ver int64, q domain.TaskQuery) string {
	return fmt.Sprintf("tasks:team:%d:v%d:s=%s:a=%d:p=%d:n=%d", teamID, ver, q.Status, q.AssigneeID, q.Page, q.PageSize)
}

// cachedPage is the stored form of a domain.TaskPage, which carries no JSON tags.
type cachedPage struct {
	Tasks      []cachedTask `json:"tasks"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PageSize   int          `json:"page_size"`
	TotalPages int          `json:"total_pages"`
}

// cachedTask has exactly the fields of domain.Task, so the two convert into each other and
// the compiler rejects a task field the cache would silently drop.
type cachedTask struct {
	ID          int64               `json:"id"`
	TeamID      int64               `json:"team_id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Status      domain.TaskStatus   `json:"status"`
	Priority    domain.TaskPriority `json:"priority"`
	CreatorID   int64               `json:"creator_id"`
	AssigneeID  *int64              `json:"assignee_id"`
	DueDate     *time.Time          `json:"due_date"`
	CompletedAt *time.Time          `json:"completed_at"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

func newCachedPage(page *domain.TaskPage) cachedPage {
	tasks := make([]cachedTask, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		tasks = append(tasks, cachedTask(task))
	}
	return cachedPage{
		Tasks:      tasks,
		Total:      page.Total,
		Page:       page.Page,
		PageSize:   page.PageSize,
		TotalPages: page.TotalPages,
	}
}

func (p cachedPage) toDomain() *domain.TaskPage {
	tasks := make([]domain.Task, 0, len(p.Tasks))
	for _, task := range p.Tasks {
		tasks = append(tasks, domain.Task(task))
	}
	return &domain.TaskPage{
		Tasks:      tasks,
		Total:      p.Total,
		Page:       p.Page,
		PageSize:   p.PageSize,
		TotalPages: p.TotalPages,
	}
}
