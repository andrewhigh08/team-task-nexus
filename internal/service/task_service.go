package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// TaskDeps lists the collaborators of TaskService. A nil Clock means time.Now.
type TaskDeps struct {
	Tasks   port.TaskRepository
	History port.TaskHistoryRepository
	Teams   port.TeamRepository
	Cache   port.TaskListCache
	Outbox  port.OutboxRepository
	Tx      port.TxManager
	Clock   func() time.Time
}

// TaskService implements port.TaskService. Assigning a task to someone, on creation or later,
// writes a task.assigned event to the outbox in the transaction of the change.
type TaskService struct {
	tasks   port.TaskRepository
	history port.TaskHistoryRepository
	teams   port.TeamRepository
	cache   port.TaskListCache
	outbox  port.OutboxRepository
	tx      port.TxManager
	now     func() time.Time
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
	case deps.Cache == nil:
		missing = "task list cache"
	case deps.Outbox == nil:
		missing = "outbox repository"
	case deps.Tx == nil:
		missing = "transaction manager"
	}
	if missing != "" {
		return nil, errors.New("task service: " + missing + " is required")
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &TaskService{
		tasks:   deps.Tasks,
		history: deps.History,
		teams:   deps.Teams,
		cache:   deps.Cache,
		outbox:  deps.Outbox,
		tx:      deps.Tx,
		now:     deps.Clock,
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
		var err error
		task, err = s.create(ctx, userID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.invalidateTeam(ctx, task.TeamID)
	return task, nil
}

// create is the transactional part of Create.
func (s *TaskService) create(ctx context.Context, userID int64, in port.CreateTaskInput) (*domain.Task, error) {
	if _, err := requireMember(ctx, s.teams, in.TeamID, userID); err != nil {
		return nil, err
	}
	if in.AssigneeID != nil {
		if err := requireAssignable(ctx, s.teams, in.TeamID, *in.AssigneeID); err != nil {
			return nil, err
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
		return nil, err
	}
	// The row is read back for the timestamps the database assigned.
	task, err := s.tasks.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.AssigneeID != nil {
		if err := s.recordAssignment(ctx, task, userID); err != nil {
			return nil, err
		}
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
	task, err := s.save(ctx, &updated, changes, userID)
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

// save writes the updated task and the history of the changes userID made, and returns the
// task as stored. A task can only be assigned to a member of its team; the new assignee, but
// nobody whose assignment was merely cleared, is told about it.
func (s *TaskService) save(ctx context.Context, task *domain.Task, changes []domain.TaskChange, userID int64) (*domain.Task, error) {
	assigned := task.AssigneeID != nil && changesField(changes, fieldAssignee)
	if assigned {
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
	if assigned {
		if err := s.recordAssignment(ctx, task, userID); err != nil {
			return nil, err
		}
	}
	return s.tasks.GetByID(ctx, task.ID)
}

// recordAssignment writes the task.assigned event of task, assigned by actorID, to the outbox
// in the transaction carried by ctx.
func (s *TaskService) recordAssignment(ctx context.Context, task *domain.Task, actorID int64) error {
	event, err := taskAssignedEvent(domain.TaskAssigned{
		TaskID:     task.ID,
		TeamID:     task.TeamID,
		Title:      task.Title,
		AssigneeID: *task.AssigneeID,
		ActorID:    actorID,
	})
	if err != nil {
		return err
	}
	return s.outbox.Add(ctx, event)
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
	return s.listTeam(ctx, q)
}

// listTeam serves a list of one team from the cache and fills the cache on a miss. The version
// is read before the database is queried: should a write commit in between, the page is stored
// under the version that write retires, where no reader will look for it again. Without a
// version the cache is bypassed altogether.
func (s *TaskService) listTeam(ctx context.Context, q domain.TaskQuery) (*domain.TaskPage, error) {
	ver, err := s.cache.Version(ctx, q.TeamID)
	if err != nil {
		cacheFailed(ctx, "version", q.TeamID, err)
		return s.listFromStore(ctx, q)
	}

	cached, found, err := s.cache.Get(ctx, q.TeamID, ver, q)
	switch {
	case err != nil:
		cacheFailed(ctx, "get", q.TeamID, err)
	case found:
		metrics.RecordCacheRequest(metrics.CacheTaskList, metrics.ResultHit)
		return cached, nil
	default:
		metrics.RecordCacheRequest(metrics.CacheTaskList, metrics.ResultMiss)
	}

	page, err := s.listFromStore(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := s.cache.Set(ctx, q.TeamID, ver, q, page); err != nil {
		cacheFailed(ctx, "set", q.TeamID, err)
	}
	return page, nil
}

// normalizeTaskQuery fills in the default page and page size of q and validates it.
func normalizeTaskQuery(q *domain.TaskQuery) error {
	errs := fieldErrors{}
	normalizePage(&q.Page, &q.PageSize, errs)
	if q.TeamID < 0 {
		errs.add("team_id", msgPositive)
	}
	if q.AssigneeID < 0 {
		errs.add(fieldAssignee, msgPositive)
	}
	if q.Status != "" {
		errs.checkStatus(fieldStatus, q.Status)
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
		TotalPages: totalPages(total, q.PageSize),
	}, nil
}

// History returns a page of the change history of the task to a member of its team.
func (s *TaskService) History(ctx context.Context, userID, taskID int64, p domain.PageRequest) (*domain.TaskChangePage, error) {
	if err := normalizePageRequest(&p); err != nil {
		return nil, err
	}
	if _, err := loadTask(ctx, s.tasks, s.teams, taskID, userID); err != nil {
		return nil, err
	}
	changes, total, err := s.history.ListByTask(ctx, taskID, p)
	if err != nil {
		return nil, err
	}
	return &domain.TaskChangePage{
		Changes:    changes,
		Total:      total,
		Page:       p.Page,
		PageSize:   p.PageSize,
		TotalPages: totalPages(total, p.PageSize),
	}, nil
}

// ListOrphaned returns the tasks of the team still assigned to users who left it, to the
// team's owner and admins.
func (s *TaskService) ListOrphaned(ctx context.Context, userID, teamID int64) ([]domain.OrphanedAssignment, error) {
	if _, err := requireManager(ctx, s.teams, teamID, userID); err != nil {
		return nil, err
	}
	return s.tasks.ListOrphaned(ctx, teamID)
}

// invalidateTeam retires the cached task lists of the team after a committed change. The change
// stays committed when the client gives up on the answer, so the call ignores the request's
// cancellation; the Redis client's timeouts still bound it. A failure leaves stale pages until
// they expire, which is not worth failing a committed write for.
func (s *TaskService) invalidateTeam(ctx context.Context, teamID int64) {
	if err := s.cache.Invalidate(context.WithoutCancel(ctx), teamID); err != nil {
		cacheFailed(ctx, "invalidate", teamID, err)
	}
}

// cacheFailed logs and counts a failed operation on the task list cache. The cache only saves
// database work, so its failures never reach the client.
func cacheFailed(ctx context.Context, operation string, teamID int64, err error) {
	metrics.RecordCacheRequest(metrics.CacheTaskList, metrics.ResultError)
	logger.FromContext(ctx).Warn("task list cache failed", "operation", operation, "team_id", teamID, "error", err)
}
