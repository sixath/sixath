# Runbook：Portal 不可用

## 症状

- Gateway 日志 `webhook_resolve_failed` / `portal_requests_total{status="error"|"http_5xx"}` 突增。
- Web 管理台请求 5xx / 超时。

## 排查

1. 确认进程与端口：`docker compose ps`、`curl -s localhost:<portal>/healthz`（或 `/metrics`）。
2. 看 Gateway 视角：`gateway_portal_requests_total{status=~"error|http_.*"}` 是否爬升。
3. 看 Portal 自身：`agent_requests_total{status="error"}`、`executor_errors_total`（若涉及数据源）。
4. 看依赖：MySQL 是否存活（连接池指标 `datasource_pool_in_use`/`idle`）、模型 provider 是否限流（429）。

## 处置

- MySQL/依赖故障 → 先恢复依赖，Portal 通常自动恢复。
- 模型 provider 429/5xx → 确认重试装饰器在生效（`SATH_MODEL_RETRY_MAX_ATTEMPTS` 未设成 1）；必要时降级到备用 provider。
- 进程崩溃 → 看 `slog` 错误、`turn_trace` 最后写入；用 `docker compose up -d portal` 重启。

## 恢复确认

- Gateway `gateway_portal_requests_total{status="ok"}` 恢复增长，error 率回落。
- Web 管理台可正常创建会话。
