package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/shalfey088/team-task-nexus/internal/domain"
)

type NotificationServiceImpl struct {
	mu            sync.Mutex
	failures      int
	lastFailure   time.Time
	threshold     int
	resetInterval time.Duration
}

func NewNotificationService() *NotificationServiceImpl {
	return &NotificationServiceImpl{
		threshold:     3,
		resetInterval: 30 * time.Second,
	}
}

func (s *NotificationServiceImpl) isCircuitOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failures >= s.threshold {
		if time.Since(s.lastFailure) > s.resetInterval {
			s.failures = 0
			return false
		}
		return true
	}
	return false
}

func (s *NotificationServiceImpl) NotifyTaskAssigned(ctx context.Context, task *domain.Task, assignee *domain.User) error {
	if s.isCircuitOpen() {
		log.Printf("[NOTIFICATION] Circuit breaker open, skipping notification for task %d", task.ID)
		return nil
	}

	log.Printf("[NOTIFICATION] Mock email: Task '%s' (ID: %d) assigned to %s (%s)",
		task.Title, task.ID, assignee.FullName, assignee.Email)
	return nil
}

func (s *NotificationServiceImpl) NotifyCommentAdded(ctx context.Context, comment *domain.TaskComment, task *domain.Task) error {
	if s.isCircuitOpen() {
		log.Printf("[NOTIFICATION] Circuit breaker open, skipping notification for comment on task %d", task.ID)
		return nil
	}

	log.Printf("[NOTIFICATION] Mock email: New comment on task '%s' (ID: %d) by user %d",
		task.Title, task.ID, comment.UserID)
	return nil
}
