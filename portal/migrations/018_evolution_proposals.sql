-- Evolution proposals: skill self-evolution review queue.
-- All writes go through human review on /evolution-review page.

CREATE TABLE IF NOT EXISTS evolution_proposals (
    id                  VARCHAR(36)   NOT NULL PRIMARY KEY,
    agent_id            VARCHAR(36)   NOT NULL,
    session_id          VARCHAR(36)   NOT NULL,
    turn_index          INT           NOT NULL DEFAULT 0,
    signal_type         VARCHAR(32)   NOT NULL,
    confidence          DECIMAL(3,2)  NOT NULL DEFAULT 0.00,
    problem_summary     TEXT          NOT NULL,
    proposed_content    TEXT          NOT NULL,
    target_path         VARCHAR(512)  NOT NULL DEFAULT '',
    target_action       VARCHAR(32)   NOT NULL DEFAULT 'create',
    conflict            TINYINT(1)    NOT NULL DEFAULT 0,
    conflict_detail     TEXT          NULL,
    conflict_check_failed TINYINT(1)  NOT NULL DEFAULT 0,
    dedup_skipped       TINYINT(1)    NOT NULL DEFAULT 0,
    status              VARCHAR(16)   NOT NULL DEFAULT 'pending',
    review_comment      TEXT          NULL,
    created_at          DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    reviewed_at         DATETIME(3)   NULL,
    reviewed_by         VARCHAR(64)   NULL,
    INDEX idx_ep_status (status),
    INDEX idx_ep_agent (agent_id),
    INDEX idx_ep_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;