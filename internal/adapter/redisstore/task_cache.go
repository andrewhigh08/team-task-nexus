// Package redisstore implements the cache and rate limiter ports on Redis.
package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

const taskCacheTTL = 5 * time.Minute

// TaskCache caches pages of task lists.
type TaskCache struct {
	client *redis.Client
}

// NewTaskCache returns a TaskCache on client.
func NewTaskCache(client *redis.Client) *TaskCache {
	return &TaskCache{client: client}
}

// cacheKey identifies the page q selects within its team. The viewer is not part of it: every
// member of a team sees the same tasks of that team.
func (c *TaskCache) cacheKey(q domain.TaskQuery) string {
	return fmt.Sprintf("tasks:team:%d:status:%s:assignee:%d:page:%d:size:%d",
		q.TeamID, q.Status, q.AssigneeID, q.Page, q.PageSize)
}

// GetTaskList returns the cached page for q, or nil when there is none.
func (c *TaskCache) GetTaskList(ctx context.Context, q domain.TaskQuery) (*domain.TaskPage, error) {
	data, err := c.client.Get(ctx, c.cacheKey(q)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}

	var page domain.TaskPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// SetTaskList caches page as the result of q.
func (c *TaskCache) SetTaskList(ctx context.Context, q domain.TaskQuery, page *domain.TaskPage) error {
	data, err := json.Marshal(page)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.cacheKey(q), data, taskCacheTTL).Err()
}

// InvalidateTeam drops every cached page of the team's tasks.
func (c *TaskCache) InvalidateTeam(ctx context.Context, teamID int64) error {
	pattern := fmt.Sprintf("tasks:team:%d:*", teamID)
	iter := c.client.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return c.client.Del(ctx, keys...).Err()
	}
	return nil
}
