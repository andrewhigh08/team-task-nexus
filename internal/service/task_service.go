package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// Paging of task lists. maxPage keeps the OFFSET the repository computes far from overflow;
// deeper pages are better reached by narrowing the filter.
const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPage         = 10_000
)

// TaskDeps lists the collaborators of TaskService. A nil Clock means time.Now.
type TaskDeps struct {
	Tasks    port.TaskRepository
	History  port.TaskHistoryRepository
	Teams    port.TeamRepository
	Users    port.UserRepository
	Cache    port.TaskCache
	Tx       port.TxManager
	Notifier port.NotificationService
	Clock    func() time.Time
}

// TaskService implements port.TaskService.
type TaskService struct {
	tasks    port.TaskRepository
	history  port.TaskHistoryRepository
	teams    port.TeamRepository
	users    port.UserRepository
	cache    port.TaskCache
	tx       port.TxManager
	notifier port.NotificationService
	now      func() time.Time
}

// NewTaskService validates deps and returns a TaskService.
func NewTaskService(deps TaskDeps) (*TaskService, error) {
	var missing string
	switch {
	case deps.Tasks == nil:
		missing = "task repository"
	case deps.History == nil:
		missing = "task history repository"
	case deps.Teams == nil:
		missing = "team repository"
	case deps.Users == nil:
		missing = "user repository"
	case deps.Cache == nil:
		missing = "task cache"
	case deps.Tx == nil:
		missing = "transaction manager"
	case deps.Notifier == nil:
		missing = "notification service"
	}
	if missing != "" {
		return nil, errors.New("task service: " + missing + " is required")
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &TaskService{
		tasks:    deps.Tasks,
		history:  deps.History,
		teams:    deps.Teams,
		users:    deps.Users,
		cache:    deps.Cache,
		tx:       deps.Tx,
		notifier: deps.Notifier,
		now:      deps.Clock,
	}, nil
}

// Create creates a task in a team of userID. The caller's and the assignee's memberships are
// checked in the transaction that inserts the task.
func (s *TaskService) Create(ctx context.Context, userID int64, in port.CreateTaskInput) (*domain.Task, error) {
	if err := normalizeNewTask(&in); err != nil {
		return nil, err
	}

	var task *domain.Task
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := requireMember(ctx, s.teams, in.TeamID, userID); err != nil {
			return err
		}
		if in.AssigneeID != nil {
			if err := requireAssignable(ctx, s.teams, in.TeamID, *in.AssigneeID); err != nil {
				return err
			}
		}
		id, err := s.tasks.Create(ctx, &domain.Task{
			TeamID:      in.TeamID,
			Title:       in.Title,
			Description: in.Description,
			Status:      domain.TaskStatusTodo,
			Priority:    *in.Priority,
			CreatorID:   userID,
			AssigneeID:  in.AssigneeID,
			DueDate:     in.DueDate,
		})
		if err != nil {
			return err
		}
		// The row is read back for the timestamps the database assigned.
		task, err = s.tasks.GetByID(ctx, id)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.invalidateTeam(ctx, task.TeamID)
	if task.AssigneeID != nil {
		s.notifyAssigned(ctx, task)
	}
	return task, nil
}

// normalizeNewTask trims the text of in, fills in the default priority and validates it.
func normalizeNewTask(in *port.CreateTaskInput) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if in.Priority == nil {
		in.Priority = new(domain.TaskPriorityMedium)
	}
	errs := fieldErrors{}
	errs.checkPositive("team_id", in.TeamID)
	errs.checkRequiredText(fieldTitle, in.Title, maxTaskTitleChars)
	errs.checkOptionalText(fieldDescription, in.Description, maxTaskDescriptionChars)
	errs.checkPriority(fieldPriority, *in.Priority)
	if in.AssigneeID != nil {
		errs.checkPositive(fieldAssignee, *in.AssigneeID)
	}
	return errs.err()
}

// Get returns the task to a member of its team.
func (s *TaskService) Get(ctx context.Context, userID, taskID int64) (*domain.Task, error) {
	return loadTask(ctx, s.tasks, s.teams, taskID, userID)
}

// Update applies in to the task inside a transaction that holds the task's row lock, so
// concurrent updates apply one after the other and each records the values it replaced.
func (s *TaskService) Update(ctx context.Context, userID, taskID int64, in port.UpdateTaskInput) (*domain.Task, error) {
	if err := normalizeTaskUpdate(&in); err != nil {
		return nil, err
	}

	var (
		task    *domain.Task
		changed bool
	)
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		task, changed, err = s.update(ctx, userID, taskID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.invalidateTeam(ctx, task.TeamID)
	}
	return task, nil
}

// update is the transactional part of Update. It reports whether the task changed; an update
// that changes no field writes neither the task nor its history.
func (s *TaskService) update(ctx context.Context, userID, taskID int64, in port.UpdateTaskInput) (*domain.Task, bool, error) {
	current, err := s.lockTask(ctx, userID, taskID)
	if err != nil {
		return nil, false, err
	}
	updated := *current
	changes := applyTaskUpdate(&updated, in, userID, s.now())
	if len(changes) == 0 {
		return current, false, nil
	}
	task, err := s.save(ctx, &updated, changes)
	if err != nil {
		return nil, false, err
	}
	return task, true, nil
}

// lockTask locks the task's row for the rest of the transaction, if userID may see the task.
func (s *TaskService) lockTask(ctx context.Context, userID, taskID int64) (*domain.Task, error) {
	task, err := s.tasks.GetByIDForUpdate(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := requireTaskAccess(ctx, s.teams, task, userID); err != nil {
		return nil, err
	}
	return task, nil
}

// save writes the updated task and the history of changes, and returns the task as stored.
// A task can only be assigned to a member of its team.
func (s *TaskService) save(ctx context.Context, task *domain.Task, changes []domain.TaskChange) (*domain.Task, error) {
	if task.AssigneeID != nil && changesField(changes, fieldAssignee) {
		if err := requireAssignable(ctx, s.teams, task.TeamID, *task.AssigneeID); err != nil {
			return nil, err
		}
	}
	if err := s.tasks.Update(ctx, task); err != nil {
		return nil, err
	}
	if err := s.history.Add(ctx, changes); err != nil {
		return nil, err
	}
	return s.tasks.GetByID(ctx, task.ID)
}

func changesField(changes []domain.TaskChange, field string) bool {
	return slices.ContainsFunc(changes, func(c domain.TaskChange) bool { return c.Field == field })
}

// List returns a page of the tasks of q.ViewerID's teams. Lists of a single team are served
// from the cache when possible; lists across teams differ per viewer and are never cached.
func (s *TaskService) List(ctx context.Context, q domain.TaskQuery) (*domain.TaskPage, error) {
	if err := normalizeTaskQuery(&q); err != nil {
		return nil, err
	}
	if q.TeamID == 0 {
		return s.listFromStore(ctx, q)
	}

	if _, err := requireMember(ctx, s.teams, q.TeamID, q.ViewerID); err != nil {
		return nil, err
	}
	cached, err := s.cache.GetTaskList(ctx, q)
	if err != nil {
		logger.FromContext(ctx).Warn("task list cache read failed", "team_id", q.TeamID, "error", err)
	}
	if cached != nil {
		return cached, nil
	}
	page, err := s.listFromStore(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := s.cache.SetTaskList(ctx, q, page); err != nil {
		logger.FromContext(ctx).Warn("task list cache write failed", "team_id", q.TeamID, "error", err)
	}
	return page, nil
}

// normalizeTaskQuery fills in the default page and page size of q and validates it.
func normalizeTaskQuery(q *domain.TaskQuery) error {
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = defaultPageSize
	}
	errs := fieldErrors{}
	if q.TeamID < 0 {
		errs.add("team_id", msgPositive)
	}
	if q.AssigneeID < 0 {
		errs.add(fieldAssignee, msgPositive)
	}
	if q.Status != "" {
		errs.checkStatus(fieldStatus, q.Status)
	}
	if q.Page < 1 || q.Page > maxPage {
		errs.add("page", fmt.Sprintf("must be between 1 and %d", maxPage))
	}
	if q.PageSize < 1 || q.PageSize > maxPageSize {
		errs.add("page_size", fmt.Sprintf("must be between 1 and %d", maxPageSize))
	}
	return errs.err()
}

func (s *TaskService) listFromStore(ctx context.Context, q domain.TaskQuery) (*domain.TaskPage, error) {
	tasks, total, err := s.tasks.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return &domain.TaskPage{
		Tasks:      tasks,
		Total:      total,
		Page:       q.Page,
		PageSize:   q.PageSize,
		TotalPages: (total + q.PageSize - 1) / q.PageSize,
	}, nil
}

// History returns the change history of the task to a member of its team.
func (s *TaskService) History(ctx context.Context, userID, taskID int64) ([]domain.TaskChange, error) {
	if _, err := loadTask(ctx, s.tasks, s.teams, taskID, userID); err != nil {
		return nil, err
	}
	return s.history.ListByTask(ctx, taskID)
}

// ListOrphaned returns the tasks of the team still assigned to users who left it, to the
// team's owner and admins.
func (s *TaskService) ListOrphaned(ctx context.Context, userID, teamID int64) ([]domain.OrphanedAssignment, error) {
	if _, err := requireManager(ctx, s.teams, teamID, userID); err != nil {
		return nil, err
	}
	return s.tasks.ListOrphaned(ctx, teamID)
}

// invalidateTeam drops the cached task lists of the team after a committed change. A failure
// leaves stale pages until they expire, which is not worth failing a committed write for.
func (s *TaskService) invalidateTeam(ctx context.Context, teamID int64) {
	if err := s.cache.InvalidateTeam(ctx, teamID); err != nil {
		logger.FromContext(ctx).Warn("task list cache invalidation failed", "team_id", teamID, "error", err)
	}
}

// notifyAssigned tells the assignee of a newly created task about it. The task is already
// committed, so failures are logged rather than returned.
func (s *TaskService) notifyAssigned(ctx context.Context, task *domain.Task) {
	log := logger.FromContext(ctx)
	assignee, err := s.users.GetByID(ctx, *task.AssigneeID)
	if err != nil {
		log.Warn("load assignee for notification failed", "task_id", task.ID, "error", err)
		return
	}
	if err := s.notifier.NotifyTaskAssigned(ctx, task, assignee); err != nil {
		log.Warn("task assignment notification failed", "task_id", task.ID, "error", err)
	}
}
