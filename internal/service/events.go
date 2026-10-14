package service

import (
	"encoding/json"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// The JSON payloads of the events. Their field names are a contract with the consumers of the
// events; the fields mirror the domain payloads, so each converts from its domain type.

type taskAssignedPayload struct {
	TaskID     int64  `json:"task_id"`
	TeamID     int64  `json:"team_id"`
	Title      string `json:"title"`
	AssigneeID int64  `json:"assignee_id"`
	ActorID    int64  `json:"actor_id"`
}

type taskCommentedPayload struct {
	TaskID         int64  `json:"task_id"`
	TeamID         int64  `json:"team_id"`
	CommentID      int64  `json:"comment_id"`
	AuthorID       int64  `json:"author_id"`
	TaskCreatorID  int64  `json:"task_creator_id"`
	TaskAssigneeID *int64 `json:"task_assignee_id"`
}

func taskAssignedEvent(p domain.TaskAssigned) (domain.NewEvent, error) {
	return newEvent(domain.EventTaskAssigned, taskAssignedPayload(p))
}

func taskCommentedEvent(p domain.TaskCommented) (domain.NewEvent, error) {
	return newEvent(domain.EventTaskCommented, taskCommentedPayload(p))
}

func newEvent(typ domain.EventType, payload any) (domain.NewEvent, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return domain.NewEvent{}, apperror.Internal("encode "+string(typ)+" event", err)
	}
	return domain.NewEvent{Type: typ, Payload: data}, nil
}
