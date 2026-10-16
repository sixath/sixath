-- 历史遗留的 utf8(3字节) 表转 utf8mb4，修复含 emoji(4字节) 文本写入报 MySQL 1366。
-- 这些表在 DSN 加上 charset=utf8mb4 之前建库，AutoMigrate 不改已有表/列字符集。
-- 排序规则用 utf8mb4_unicode_ci，与库默认及 tools 等表一致。
-- utf8mb4_general_ci 会让 tools.id = agent_tools.tool_id 报 MySQL 1267。
-- 代码层已在 NewData 启动时幂等执行相同 CONVERT（见 internal/data/data.go ensureUTF8MB4），
-- 此文件仅作文档性记录；如需手动修复，逐条执行即可。
ALTER TABLE cron_run_history CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE cron_tasks      CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE channels        CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE channel_runtime_status CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE agent_tools     CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE chat_growth_states    CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE growth_curator_states CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
ALTER TABLE growth_workspace_leases CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
