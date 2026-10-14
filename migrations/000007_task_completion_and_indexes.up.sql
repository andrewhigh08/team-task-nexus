-- Earlier versions accepted any priority; clamp stray values so the CHECK below can be added.
-- Setting updated_at to itself keeps ON UPDATE from stamping data repairs as user edits.
UPDATE tasks
SET priority = LEAST(GREATEST(priority, 1), 3), updated_at = updated_at
WHERE priority NOT BETWEEN 1 AND 3;

-- (team_id, created_at, id) serves the team-scoped, newest-first task list and replaces
-- idx_tasks_team; (team_id, completed_at) serves the analytics. idx_tasks_status is not
-- selective enough to be used on its own.
ALTER TABLE tasks
    MODIFY COLUMN due_date DATE NULL,
    ADD COLUMN completed_at DATETIME NULL AFTER due_date,
    ADD CONSTRAINT chk_tasks_priority CHECK (priority BETWEEN 1 AND 3),
    ADD INDEX idx_tasks_team_created (team_id, created_at, id),
    ADD INDEX idx_tasks_team_completed (team_id, completed_at),
    DROP INDEX idx_tasks_team,
    DROP INDEX idx_tasks_status;

-- The last edit of a task that is already done is the best estimate of its completion.
UPDATE tasks
SET completed_at = updated_at, updated_at = updated_at
WHERE status = 'done';

-- Covered by the (task_id, changed_at) and (task_id, created_at) indexes.
ALTER TABLE task_history DROP INDEX idx_history_task;
ALTER TABLE task_comments DROP INDEX idx_comments_task;
