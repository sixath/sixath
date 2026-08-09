-- 003_add_growth_retry_count.sql
-- spec phase2 §A5：失败重试次数，超过阈值后 worker 自动清理 pending。
ALTER TABLE chat_growth_states
    ADD COLUMN review_retry_count INT NOT NULL DEFAULT 0 AFTER review_failed_at;
