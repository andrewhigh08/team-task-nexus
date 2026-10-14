package domain

import "time"

// EventType names a kind of event that other systems are told about.
type EventType string

// Event types.
const (
	EventTaskAssigned  EventType = "task.assigned"
	EventTaskCommented EventType = "task.commented"
)

// TaskAssigned is the payload of a task.assigned event: ActorID assigned the task to
// AssigneeID, when creating it or later.
type TaskAssigned struct {
	TaskID     int64
	TeamID     int64
	Title      string
	AssigneeID int64
	ActorID    int64
}

// TaskCommented is the payload of a task.commented event: AuthorID commented on the task. It
// names the task's creator and assignee, the users a consumer is most likely to notify;
// TaskAssigneeID is nil for an unassigned task.
type TaskCommented struct {
	TaskID         int64
	TeamID         int64
	CommentID      int64
	AuthorID       int64
	TaskCreatorID  int64
	TaskAssigneeID *int64
}

// NewEvent is an event about to be written to the outbox. Payload is its JSON encoding.
type NewEvent struct {
	Type    EventType
	Payload []byte
}

// OutboxEvent is an event stored in the outbox, as handed out for delivery. ID identifies the
// event to its consumers, who may see it more than once. Attempts counts the deliveries
// including the current one; CreatedAt is when the event was written, in the transaction of
// the change it describes.
type OutboxEvent struct {
	ID        int64
	Type      EventType
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}
