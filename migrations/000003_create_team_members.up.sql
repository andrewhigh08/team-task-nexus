CREATE TABLE team_members (
    team_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role ENUM('owner', 'admin', 'member') NOT NULL DEFAULT 'member',
    UNIQUE INDEX idx_team_members_unique (team_id, user_id),
    INDEX idx_team_members_user (user_id),
    CONSTRAINT fk_team_members_team FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    CONSTRAINT fk_team_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
