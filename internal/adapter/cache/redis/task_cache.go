package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shalfey088/team-task-nexus/internal/domain"
)

const taskCacheTTL = 5 * time.Minute

type TaskCache struct {
	client *redis.Client
}

func NewTaskCache(client *redis.Client) *TaskCache {
	return &TaskCache{client: client}
}

func (c *TaskCache) cacheKey(filter domain.TaskFilter) string {
	return fmt.Sprintf("tasks:team:%d:status:%s:assignee:%d:page:%d:size:%d",
		filter.TeamID, filter.Status, filter.AssigneeID, filter.Page, filter.PageSize)
}

func (c *TaskCache) GetTaskList(ctx context.Context, filter domain.TaskFilter) (*domain.TaskListResponse, error) {
	data, err := c.client.Get(ctx, c.cacheKey(filter)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var response domain.TaskListResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *TaskCache) SetTaskList(ctx context.Context, filter domain.TaskFilter, response *domain.TaskListResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.cacheKey(filter), data, taskCacheTTL).Err()
}

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
