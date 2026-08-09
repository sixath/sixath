-- 001_create_chat_growth_states.sql
-- Growth state machine cursor per chat session (spec §4).
CREATE TABLE chat_growth_states (
    session_id               VARCHAR(36)   NOT NULL PRIMARY KEY,
    tool_iters_since_review  INT           NOT NULL DEFAULT 0,
    turns_since_memory_review INT          NOT NULL DEFAULT 0,
    pending_skill_review     TINYINT(1)    NOT NULL DEFAULT 0,
    pending_memory_review    TINYINT(1)    NOT NULL DEFAULT 0,
    last_skill_error         TEXT,
    last_memory_error        TEXT,
    review_failed_at         DATETIME(3),
    last_idle_check_at       DATETIME(3),
    created_at               DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at               DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE,
    INDEX idx_chat_growth_states_pending_skill (pending_skill_review),
    INDEX idx_chat_growth_states_pending_memory (pending_memory_review),
    INDEX idx_chat_growth_states_last_idle (last_idle_check_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
