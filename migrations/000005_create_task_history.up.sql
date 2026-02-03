CREATE TABLE task_history (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    field VARCHAR(50) NOT NULL,
    old_value TEXT NOT NULL,
    new_value TEXT NOT NULL,
    changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_history_task (task_id),
    INDEX idx_history_user (user_id),
    INDEX idx_history_task_time (task_id, changed_at),
    CONSTRAINT fk_history_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    CONSTRAINT fk_history_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
