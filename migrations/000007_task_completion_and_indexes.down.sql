ALTER TABLE task_comments ADD INDEX idx_comments_task (task_id);
ALTER TABLE task_history ADD INDEX idx_history_task (task_id);

ALTER TABLE tasks
    ADD INDEX idx_tasks_team (team_id),
    ADD INDEX idx_tasks_status (status),
    DROP INDEX idx_tasks_team_created,
    DROP INDEX idx_tasks_team_completed,
    DROP CHECK chk_tasks_priority,
    DROP COLUMN completed_at,
    MODIFY COLUMN due_date TIMESTAMP NULL;
