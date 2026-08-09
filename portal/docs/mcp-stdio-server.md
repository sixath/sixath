# MCP Server / Stdio — 验收说明

实现分支：`feat/mcp-stdio-server`（`framework` / `portal` / `web` 三仓）。

## 已自动化覆盖

```bash
cd framework && go test ./tool/ -run 'TestMcpProcessPool|TestStdioMcp_ListAndCall|TestValidateStdioMcp' -count=1
cd portal && go test ./internal/biz/ ./internal/chat/ -run 'McpServer|BuildRegistry' -count=1
```

含：白名单（含 `--eval=`）、进程池复用/指纹/idle/活跃 lease、node fixture List+Call、掩码、BuildRegistry 绑定时注册失败报错。

## 上线前手工步骤

1. 应用迁移 `portal/migrations/013_mcp_servers.sql`（或依赖 AutoMigrate）。
2. 重启 portal（确保 `replace => ../framework` 指向本分支）。
3. Web → **MCP 服务** → 新建：
   - id: `confluence`
   - transport: `stdio`
   - command: `npx`
   - args: `-y` / `@atlassian-dc-mcp/confluence`（一行一个）
   - env: `CONFLUENCE_HOST`, `CONFLUENCE_API_TOKEN`
4. **测试连接** → 应出现 `confluence_*` 工具名。
5. Agent 详情 → 勾选该 MCP 服务 → 保存绑定。
6. 对话中调用搜索/读页；idle 内第二轮应复用进程（日志可搜 pool hit）。
7. Get 回显 Token 为 `***`；再保存不丢；`command=bash` 创建被拒。

## 非目标（未做）

Confluence → KnowledgeProvider / wiki ingest；密钥 KMS；旧 MCP Tool 强制迁移。
