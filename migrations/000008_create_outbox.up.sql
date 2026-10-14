-- Events written in the transaction of the change they describe and delivered afterwards by
-- the dispatcher. A row is pending until sent_at or failed_at is set; next_attempt_at holds a
-- pending row back while a dispatcher holds its lease or until its next retry is due.
CREATE TABLE outbox (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_type VARCHAR(64) NOT NULL,
    payload JSON NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    last_error VARCHAR(1024) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    sent_at DATETIME(3) NULL,
    failed_at DATETIME(3) NULL,
    INDEX idx_outbox_pending (sent_at, failed_at, next_attempt_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
