-- portal/migrations/024_cron_delivery_peer.sql
-- Cron 投递到 weixin_ilink 渠道时的接收人（docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md §5.4）。
-- 启动时 GORM AutoMigrate 自动加列；本文件仅作手工修复与记录。
ALTER TABLE cron_tasks
  ADD COLUMN delivery_peer_id VARCHAR(128) NULL;
