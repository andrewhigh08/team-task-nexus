package domain

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTaskStatus_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status TaskStatus
		want   bool
	}{
		{TaskStatusTodo, true},
		{TaskStatusInProgress, true},
		{TaskStatusReview, true},
		{TaskStatusDone, true},
		{"", false},
		{"Done", false},
		{"archived", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}

func TestTaskPriority_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		priority TaskPriority
		want     bool
	}{
		{0, false},
		{TaskPriorityLow, true},
		{TaskPriorityMedium, true},
		{TaskPriorityHigh, true},
		{4, false},
		{-1, false},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(int(tt.priority)), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.priority.Valid())
		})
	}
}

func TestTask_SetStatus(t *testing.T) {
	t.Parallel()
	earlier := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 14, 9, 26, 53, 0, time.FixedZone("UTC+3", 3*60*60))
	tests := []struct {
		name          string
		from          TaskStatus
		completedAt   *time.Time
		to            TaskStatus
		wantCompleted *time.Time
	}{
		{name: "becoming done records the instant in UTC", from: TaskStatusReview, to: TaskStatusDone, wantCompleted: new(now.UTC())},
		{name: "staying done keeps the original instant", from: TaskStatusDone, completedAt: &earlier, to: TaskStatusDone, wantCompleted: &earlier},
		{name: "leaving done clears the instant", from: TaskStatusDone, completedAt: &earlier, to: TaskStatusInProgress},
		{name: "moving between open statuses leaves it unset", from: TaskStatusTodo, to: TaskStatusReview},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			task := Task{Status: tt.from, CompletedAt: tt.completedAt}

			task.SetStatus(tt.to, now)

			assert.Equal(t, tt.to, task.Status)
			assert.Equal(t, tt.wantCompleted, task.CompletedAt)
		})
	}
}
