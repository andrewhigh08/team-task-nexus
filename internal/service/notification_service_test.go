package service

import (
	"context"
	"testing"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestNotificationService_NotifyTaskAssigned(t *testing.T) {
	svc := NewNotificationService()

	task := &domain.Task{ID: 1, Title: "Test Task"}
	user := &domain.User{ID: 1, Email: "user@example.com", FullName: "Test User"}

	err := svc.NotifyTaskAssigned(context.Background(), task, user)
	assert.NoError(t, err)
}

func TestNotificationService_NotifyCommentAdded(t *testing.T) {
	svc := NewNotificationService()

	comment := &domain.TaskComment{ID: 1, TaskID: 1, UserID: 1}
	task := &domain.Task{ID: 1, Title: "Test Task"}

	err := svc.NotifyCommentAdded(context.Background(), comment, task)
	assert.NoError(t, err)
}

func TestNotificationService_CircuitBreaker(t *testing.T) {
	svc := NewNotificationService()
	svc.threshold = 0 // Force circuit open

	task := &domain.Task{ID: 1, Title: "Test Task"}
	user := &domain.User{ID: 1, Email: "user@example.com", FullName: "Test User"}

	// Circuit should be open, but notification still returns nil (graceful degradation)
	err := svc.NotifyTaskAssigned(context.Background(), task, user)
	assert.NoError(t, err)
}
