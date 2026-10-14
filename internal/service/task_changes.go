package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// Task fields as they are named in inputs and in the task history.
const (
	fieldTitle       = "title"
	fieldDescription = "description"
	fieldStatus      = "status"
	fieldPriority    = "priority"
	fieldAssignee    = "assignee_id"
	fieldDueDate     = "due_date"
)

// normalizeTaskUpdate trims the text fields of in and validates every field it sets.
func normalizeTaskUpdate(in *port.UpdateTaskInput) error {
	in.Title.Value = strings.TrimSpace(in.Title.Value)
	in.Description.Value = strings.TrimSpace(in.Description.Value)

	errs := fieldErrors{}
	// The first problem recorded for a field wins, so null is reported before the value checks.
	errs.checkNotNull(fieldTitle, in.Title.Null)
	errs.checkNotNull(fieldDescription, in.Description.Null)
	errs.checkNotNull(fieldStatus, in.Status.Null)
	errs.checkNotNull(fieldPriority, in.Priority.Null)
	if in.Title.Set {
		errs.checkRequiredText(fieldTitle, in.Title.Value, maxTaskTitleChars)
	}
	if in.Description.Set {
		errs.checkOptionalText(fieldDescription, in.Description.Value, maxTaskDescriptionChars)
	}
	if in.Status.Set {
		errs.checkStatus(fieldStatus, in.Status.Value)
	}
	if in.Priority.Set {
		errs.checkPriority(fieldPriority, in.Priority.Value)
	}
	if assignee := in.AssigneeID.Ptr(); assignee != nil {
		errs.checkPositive(fieldAssignee, *assignee)
	}
	return errs.err()
}

// applyTaskUpdate applies a validated in to task and returns a history entry by userID for
// every field whose value changes. Setting a field to its current value is not a change.
// Moving the task in or out of done maintains CompletedAt with now.
func applyTaskUpdate(task *domain.Task, in port.UpdateTaskInput, userID int64, now time.Time) []domain.TaskChange {
	var changes []domain.TaskChange
	changed := func(field, oldValue, newValue string) bool {
		if oldValue == newValue {
			return false
		}
		changes = append(changes, domain.TaskChange{
			TaskID: task.ID, UserID: userID, Field: field, OldValue: oldValue, NewValue: newValue,
		})
		return true
	}

	if in.Title.Set && changed(fieldTitle, task.Title, in.Title.Value) {
		task.Title = in.Title.Value
	}
	if in.Description.Set && changed(fieldDescription, task.Description, in.Description.Value) {
		task.Description = in.Description.Value
	}
	if in.Status.Set && changed(fieldStatus, string(task.Status), string(in.Status.Value)) {
		task.SetStatus(in.Status.Value, now)
	}
	if in.Priority.Set && changed(fieldPriority, formatPriority(task.Priority), formatPriority(in.Priority.Value)) {
		task.Priority = in.Priority.Value
	}
	if in.AssigneeID.Set && changed(fieldAssignee, formatID(task.AssigneeID), formatID(in.AssigneeID.Ptr())) {
		task.AssigneeID = in.AssigneeID.Ptr()
	}
	if in.DueDate.Set && changed(fieldDueDate, formatDate(task.DueDate), formatDate(in.DueDate.Ptr())) {
		task.DueDate = in.DueDate.Ptr()
	}
	return changes
}

func formatPriority(p domain.TaskPriority) string {
	return strconv.Itoa(int(p))
}

// formatID renders an optional id for the history; no id is an empty string.
func formatID(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// formatDate renders an optional date for the history; no date is an empty string.
func formatDate(date *time.Time) string {
	if date == nil {
		return ""
	}
	return date.Format(time.DateOnly)
}
