# Sixath

Go AI Agent 平台工作区：可嵌入的 Agent 运行时（framework）、后端 Portal、Web 管理台、入站 Gateway。代码均在本 monorepo 中；另含 Docker Compose、共享文档与部署编排。

## 架构总览

```text
┌──────────┐  /api (管理)     ┌─────────────────┐
│  Web UI  │─────────────────▶│     Portal      │  Agent / 工具 / MCP /
│  Vite    │                  │  :8000 管理 API │  记忆 / 渠道出站(wecom webhook)
└────┬─────┘                  └────────▲────────┘
     │ 会话 / SSE                       │ /runtime/v1
     │                                  │ (service token)
     ▼                                  │
┌─────────────────┐                     │
│     Gateway     │─────────────────────┘
│     :8088       │
│  · Web 会话代理 │
│  · Webhook 入站 │
│  · 企微长连接   │◀──── WSS ── 企微智能机器人 AI+ (openws)
└─────────────────┘
```

| 层 | 职责 |
|----|------|
| **Web** | 管理台 + 对话 UI；会话流经 Gateway；工具/模型时间线在 SSE 展示 |
| **Gateway** | 统一入站：Web / 通用 Webhook / 企微 `wecom_bot` 长连接；路由到 Portal Runtime |
| **Portal** | Agent 执行与持久化；管理 API；群 Webhook **只出站**（`send_to_wecom`） |
| **Framework** | ReAct、Tools、Skills、MCP、上下文压缩（被 Portal 嵌入） |

详细 Gateway 用法见 [`gateway/README.md`](gateway/README.md)。

## 仓库布局

| 目录 | 说明 |
|------|------|
| `framework/` | Agent 运行时（ReAct、Tools、Skills、MCP、上下文压缩） |
| `portal/` | Kratos 后端（Agent/工具/会话/渠道/Growth） |
| `web/` | React + Vite 管理 UI |
| `gateway/` | Inbound Gateway（Web / Webhook / 企微长连接） |
| `deploy/` | MySQL 初始化等部署资产 |
| `docs/` | 设计与实施计划 |

`portal/go.mod` 通过 `replace github.com/sixath/framework => ../framework` 引用本地 framework，因此 **构建 portal 镜像时须在本仓库根目录** 作为 Docker build context。

历史独立仓库（`sixath/framework`、`sixath/portal`、`sixath/web`）已吸收进本仓；日常开发与提交以本仓库为准。

## 快速开始（Docker Compose）

前置：Docker / **Docker Compose ≥ 2.20**、磁盘空间足以构建 Go 与 Node 镜像。

### 一键部署（推荐）

```bash
# Linux / macOS
./deploy/deploy.sh --build

# Windows PowerShell
.\deploy\deploy.ps1 -Build
```

脚本会：补齐 `.env` 与 `secrets/*.txt`（来自 example，并打印警告）→ `compose up` → 等待 healthy → smoke。

| 场景 | 命令 |
|------|------|
| 默认 HTTP | `./deploy/deploy.sh --build` / `.\deploy\deploy.ps1 -Build` |
| + Neo4j | 加 `--with-neo4j` / `-WithNeo4j` |
| + TLS（Caddy） | 加 `--with-tls` / `-WithTls`（`.env` 中 `DOMAIN` 必填且非 localhost） |
| 停栈保留卷 | `--down` / `-Down` |
| 仅验活 | `--smoke-only` / `-SmokeOnly` |

设计说明：[`docs/superpowers/specs/2026-08-10-docker-compose-prod-design.md`](docs/superpowers/specs/2026-08-10-docker-compose-prod-design.md)。  
密钥：见 [`secrets/README.md`](secrets/README.md)。登录用 `BOOTSTRAP_ADMIN_EMAIL` + `secrets/bootstrap_password.txt`。

裸 `docker compose up` 前须具备 `.env`（含 `COMPOSE_PATH_SEPARATOR=\|` 与三文件 `COMPOSE_FILE`）和 `secrets/*.txt`。

TLS profile **只**终结 Web UI（Caddy→web）；Gateway / 企微仍走映射的 HTTP 端口。

### 手动 Compose

```bash
# 在本仓库根目录（先 cp .env.example .env 与 secrets examples）
docker compose up --build -d
```

默认映射（避开常见本机占用端口）：

| 服务 | 宿主地址 | 容器内 |
|------|----------|--------|
| Web UI | http://localhost:18080 | `:80`（nginx：sessions → gateway，其余 `/api` → portal） |
| Gateway | http://localhost:18088 | `:8088`（入站会话 / webhook / 企微 WS 客户端） |
| Portal HTTP | http://localhost:18000 | `:8000` |
| Portal gRPC | localhost:19000 | `:9000` |
| MySQL | localhost:13306 | `:3306`（root / root，库名 `sath`） |

Portal 使用 [`portal/configs/config.docker.yaml`](portal/configs/config.docker.yaml)，DSN 指向 compose 服务名 `mysql`。启动后会 `AutoMigrate` 建表。

```bash
# 常用运维
docker compose logs -f portal
docker compose logs -f gateway
docker compose down          # 停服务，保留数据卷
docker compose down -v       # 连同 MySQL 数据一并清除
```

单独构建镜像：

```bash
docker build -f portal/Dockerfile -t portal .
docker build -f gateway/Dockerfile -t gateway .
docker build -t sixath-web ./web
```

## 本地开发（不用 Compose）

建议顺序：MySQL → Portal → Gateway → Web。

- **framework**：见 [framework README](https://github.com/sixath/framework)
- **portal**：Go 1.25+、本机 MySQL，见 [portal/README.md](portal/README.md)（开发默认 `localhost:8000`）
- **gateway**：见 [`gateway/README.md`](gateway/README.md)  
  `cd gateway && go run ./cmd/gateway -config ./configs/config.example.yaml`（`:8088` → Portal `localhost:8000`）
- **web**：`cd web && npm install && npm run dev`（Vite `:5173`；会话相关 → `localhost:8088`，其余 `/api` → `localhost:8000`）

开发时请把 portal、gateway 与 web 的端口对齐（或改 `web/vite.config.ts` 代理目标）。  
`gateway/configs/channels.yaml` 中的企微 `bot_id` / `secret` / `corp_secret` **不要提交**到 Git。

### 企微智能机器人（长连接）速览

1. 控制台选 **智能机器人 AI+ → 使用长连接**，取得 BotID + Secret。
2. 在 `gateway/configs/channels.yaml` 增加 `type: wecom_bot` 渠道并 `enabled: true`。
3. 可选配置 `corp_id` + `corp_secret`（自建应用，成员读取），将回复卡片「发起人」解析为通讯录姓名。
4. 启动 **单实例** Gateway（同一 BotID 禁止多副本同时订阅）。
5. 群 @ / 单聊发消息 → 收到含发起人、问题、答复的 stream 卡片。

完整步骤、字段表与验收清单见 [`gateway/README.md`](gateway/README.md)。

## Inbound Gateway E2E 烟雾

Compose 或本地起好 **portal + gateway** 后：

```powershell
# 默认探测 GATEWAY 18088/8088、PORTAL 18000/8000
powershell -File _neo4j_q/verify_inbound_gateway.ps1

# 或显式指定
$env:GATEWAY_URL = 'http://127.0.0.1:18088'
$env:PORTAL_URL  = 'http://127.0.0.1:18000'
$env:WEBHOOK_SECRET = 'dev-webhook-secret'
$env:CHANNEL_ID = 'demo-webhook'
$env:RUNTIME_TOKEN = 'dev-runtime-token'
# Web 多会话 / Gateway SSE（可选）
$env:AUTH_TOKEN = '<opaque bearer>'
$env:AGENT_ID = '<agent-uuid>'
powershell -File _neo4j_q/verify_inbound_gateway.ps1
```

结果写入 `_neo4j_q/verify_inbound_gateway_out.json`（`ok` + 分项 `checks`）。服务未启动时脚本会 skip 并以 exit code 2 退出；断言失败为 1。

## 相关文档

- [Gateway 架构与使用](gateway/README.md)
- [入站 Gateway 设计](docs/superpowers/specs/2026-08-09-inbound-gateway-design.md)
- [企微智能机器人长连接设计](docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md)
- [Portal 架构设计](portal/docs/architecture_design.md)
- [Hermes 能力差距 / Harness 计划](docs/superpowers/)
- [确认卡 UX 设计](docs/superpowers/specs/2026-07-13-confirm-card-ux-design.md)

## 说明

- `node_modules/`、`bin/`、本地凭证与调试产物不入库（见 `.gitignore`）。
- `tool.json`、含明文口令的部署脚本、本地 `gateway/configs/channels.yaml` 真实凭证等密钥文件不要提交。
- 旧独立仓 remote 可归档；新改动请在本仓库 `main` 上提交与推送。
