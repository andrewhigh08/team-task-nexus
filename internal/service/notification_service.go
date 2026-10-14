package service

import (
	"context"
	"sync"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// NotificationServiceImpl stands in for an email sender: it logs the notifications it would send.
type NotificationServiceImpl struct {
	mu            sync.Mutex
	failures      int
	lastFailure   time.Time
	threshold     int
	resetInterval time.Duration
}

// NewNotificationService returns a NotificationServiceImpl.
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

// NotifyTaskAssigned logs the assignment notification for assignee.
func (s *NotificationServiceImpl) NotifyTaskAssigned(ctx context.Context, task *domain.Task, assignee *domain.User) error {
	log := logger.FromContext(ctx)
	if s.isCircuitOpen() {
		log.Warn("circuit breaker open, notification skipped", "event", "task_assigned", "task_id", task.ID)
		return nil
	}

	log.Info("notification sent", "event", "task_assigned", "task_id", task.ID, "assignee_id", assignee.ID)
	return nil
}

// NotifyCommentAdded logs the notification about a new comment on task.
func (s *NotificationServiceImpl) NotifyCommentAdded(ctx context.Context, comment *domain.Comment, task *domain.Task) error {
	log := logger.FromContext(ctx)
	if s.isCircuitOpen() {
		log.Warn("circuit breaker open, notification skipped", "event", "comment_added", "task_id", task.ID)
		return nil
	}

	log.Info("notification sent", "event", "comment_added", "task_id", task.ID, "comment_id", comment.ID, "author_id", comment.UserID)
	return nil
}
