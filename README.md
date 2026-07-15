# Sixath

Go AI Agent 平台工作区：可嵌入的 Agent 运行时、后端 Portal、Web 管理台。应用代码分别在独立 Git 仓库中；本仓库放置 Docker Compose、共享文档与部署编排。

## 仓库布局

| 目录 | 仓库 | 说明 |
|------|------|------|
| [`framework/`](https://github.com/sixath/framework) | sixath/framework | Agent 运行时（ReAct、Tools、Skills、MCP、上下文压缩） |
| [`portal/`](https://github.com/sixath/portal) | sixath/portal | Kratos 后端（Agent/工具/会话/渠道/Growth） |
| [`web/`](https://github.com/sixath/web) | sixath/web | React + Vite 管理 UI |
| `deploy/` | （本仓库） | MySQL 初始化等部署资产 |
| `docs/` | （本仓库） | 跨仓设计与实施计划 |

`portal/go.mod` 通过 `replace github.com/sixath/framework => ../framework` 引用本地 framework，因此 **构建 portal 镜像时须在本仓库根目录** 作为 Docker build context。

## 快速开始（Docker Compose）

前置：Docker / Docker Compose、磁盘空间足以构建 Go 与 Node 镜像。

```bash
# 在本仓库根目录
docker compose up --build -d
```

默认映射（避开常见本机占用端口）：

| 服务 | 宿主地址 | 容器内 |
|------|----------|--------|
| Web UI | http://localhost:18080 | `:80`（nginx，`/api` → portal） |
| Portal HTTP | http://localhost:18000 | `:8000` |
| Portal gRPC | localhost:19000 | `:9000` |
| MySQL | localhost:13306 | `:3306`（root / root，库名 `sath`） |

Portal 使用 [`portal/configs/config.docker.yaml`](portal/configs/config.docker.yaml)，DSN 指向 compose 服务名 `mysql`。启动后会 `AutoMigrate` 建表。

```bash
# 常用运维
docker compose logs -f portal
docker compose down          # 停服务，保留数据卷
docker compose down -v       # 连同 MySQL 数据一并清除
```

单独构建镜像：

```bash
docker build -f portal/Dockerfile -t portal .
docker build -t sixath-web ./web
```

## 本地开发（不用 Compose）

- **framework**：见 [framework README](https://github.com/sixath/framework)
- **portal**：Go 1.25+、本机 MySQL，见 [portal/README.md](portal/README.md)（开发默认 `localhost:8000`）
- **web**：`cd web && npm install && npm run dev`（Vite `:5173`，代理 `/api` → `localhost:8000`）

开发时请把 portal 与 web 的端口对齐（或改 `web/vite.config.ts` 代理目标）。

## 相关文档

- [Portal 架构设计](portal/docs/architecture_design.md)
- [Hermes 能力差距 / Harness 计划](docs/superpowers/)
- [确认卡 UX 设计](docs/superpowers/specs/2026-07-13-confirm-card-ux-design.md)

## 说明

- 本仓忽略 `framework/`、`portal/`、`web/` 的嵌套 `.git` 内容（见 `.gitignore`），日常改代码请在对应子仓提交并 push。
- `tool.json`、含明文口令的部署脚本等本地密钥文件不会入库。
