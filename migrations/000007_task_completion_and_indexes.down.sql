-- due_date goes back to TIMESTAMP, which holds 1970-01-01 00:00:01 to 2038-01-19 03:14:07 UTC,
-- while the API takes any calendar date. Clear the due dates outside it, a day clear of either
-- end so that they convert in any session time zone, or the ALTER below fails. Setting
-- updated_at to itself keeps ON UPDATE from stamping data repairs as user edits.
UPDATE tasks
SET due_date = NULL, updated_at = updated_at
WHERE due_date NOT BETWEEN '1970-01-02' AND '2038-01-18';

-- A single ALTER is applied whole or not at all. It goes first so that a failure leaves the
-- schema of version 7 untouched and the script can run again.
ALTER TABLE tasks
    ADD INDEX idx_tasks_team (team_id),
    ADD INDEX idx_tasks_status (status),
    DROP INDEX idx_tasks_team_created,
    DROP INDEX idx_tasks_team_completed,
    DROP CHECK chk_tasks_priority,
    DROP COLUMN completed_at,
    MODIFY COLUMN due_date TIMESTAMP NULL;

ALTER TABLE task_comments ADD INDEX idx_comments_task (task_id);
ALTER TABLE task_history ADD INDEX idx_history_task (task_id);
