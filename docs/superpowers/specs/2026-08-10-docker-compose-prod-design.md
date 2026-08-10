# Docker Compose 生产向一键部署

**日期**: 2026-08-10  
**状态**: 设计已确认；待实现规划  
**目标**: 在现有 `docker-compose.yml`（mysql + portal + gateway + web）之上，交付 **Windows Docker Desktop 与 Linux 服务器共用** 的生产向一键部署：分层 Compose、`.env` + file-based Docker secrets、可选 Neo4j / TLS（Caddy）profile、bootstrap 管理员、跨平台 deploy 脚本与冒烟验活。

**关联**:
- [仓库 README · Docker Compose](../../../README.md)
- [Gateway README](../../../gateway/README.md)
- [入站 Gateway 设计](./2026-08-09-inbound-gateway-design.md)
- Portal ACL bootstrap（`portal/internal/data/bootstrap.go`，`auth.bootstrap_email` / `bootstrap_password`）

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 目标档位 | **生产向**（含 healthcheck、restart、资源上限、密钥外置、可选 TLS） |
| 运行端 | **同一套文件**：Windows Desktop + Linux；差异仅 `.env` 与 `deploy.ps1` / `deploy.sh` |
| TLS | **默认 HTTP**；`compose.tls.yml` 中 caddy 带 **`profiles: [tls]`**（需 `DOMAIN`） |
| Neo4j | **默认不启**；`compose.neo4j.yml` 中服务带 **`profiles: [neo4j]`** |
| Compose 启用方式 | **`.env` 中 `COMPOSE_PATH_SEPARATOR=\|` + 三文件 `COMPOSE_FILE` + `--profile` 开关**（Win/Linux 同一 `.env.example`） |
| 裸 `compose up` | 要求工作区已有 `secrets/*.txt`（脚本或手动从 example 复制）；**不**双轨 env/secrets |
| 密钥 | **`.env`（非敏感）+ `secrets/*.txt`（file-based Docker secrets）** |
| 管理员 | **环境变量邮箱 + secret 密码** → 现有 Portal bootstrap 幂等写入 |
| 架构方案 | **分层 Compose（方案 2）**：核心 yaml + neo4j/tls 叠加 + deploy 脚本 |
| 非目标（本迭代） | 自动备份编排、Swarm/K8s、多节点、把本机 `E:\configs\sixath` 绑进生产镜像 |

---

## 1. 问题

现状：

- 根目录已有可用的 `docker-compose.yml` 与三份 Dockerfile，README 已写 `docker compose up --build -d`。
- 缺口偏「可交付生产」：无统一 `.env`/secrets 约定、无 portal/gateway/web health 串联、无 TLS/Neo4j 开关、无一键脚本与跨平台验活、开发默认 token/密码易误用到服务器。

因此需要：

1. 不破坏现有一键 HTTP 路径的前提下，补齐生产向能力；
2. Win/Linux 同一套 Compose；可选能力用 profile 隔离；
3. 密钥不进镜像、不进 git；首次部署可登录管理台。

---

## 2. 架构

```text
                    ┌─ profile:tls ─┐
  Internet/LAN ───▶│     Caddy     │──▶ web:80
                    └───────────────┘
                              │
┌──────────┐   /api/sessions  ┌──────────┐  /runtime/v1  ┌──────────┐
│   Web    │─────────────────▶│ Gateway  │──────────────▶│  Portal  │
│  nginx   │   /api/* 其余    │  :8088   │               │  :8000   │
└──────────┘─────────────────▶└──────────┘               └────┬─────┘
                                                              │
                         ┌────────────────────────────────────┼──────────┐
                         ▼                                    ▼          ▼
                      mysql:3306                    (profile) neo4j   secrets
```

**Compose 分层（启用方式：单一权威）**

采用 **「始终加载多文件 + 服务上的 `profiles`」**：

- 仓库根 `.env.example` 使用**跨平台可移植**写法：
  - `COMPOSE_PATH_SEPARATOR=|`
  - `COMPOSE_FILE=docker-compose.yml|compose.neo4j.yml|compose.tls.yml`
  - （避免 Linux 默认 `:` 与 Windows 默认 `;` 分歧；裸 `compose` 也须复制这份 `.env`。）
- deploy 脚本在启动前确保 `.env` 存在且含上述两项；也可额外用 `docker compose -f ... -f ... -f ...` 显式传文件（与 `COMPOSE_FILE` 等价），但文档与脚本只维护**一种**推荐路径：`.env` + `--profile`。
- `neo4j` / `caddy` 服务带 `profiles: [neo4j]` / `profiles: [tls]`；**未加 `--profile` 时这些服务不起**。
- 用户命令统一为：`docker compose --profile neo4j --profile tls up -d`（可只开其中一个 profile）。
- **禁止**再提供「仅 `-f` 叠加、无 profile」的第二套用法，避免脚本与文档分叉。

| 文件 | 职责 |
|------|------|
| `docker-compose.yml` | 核心：mysql、portal、gateway、web；healthcheck、`restart: unless-stopped`、资源上限、secrets 声明 |
| `compose.neo4j.yml` | 定义 `neo4j` 服务（`profiles: [neo4j]`）+ portal 图记忆相关 env/挂载覆盖；portal 对 neo4j 的 `depends_on` **必须**写 `required: false` + `condition: service_healthy`，以便未开 `--profile neo4j` 时默认栈仍能启动（Compose **≥ 2.20**） |
| `compose.tls.yml` | 定义 `caddy`（`profiles: [tls]`）、`deploy/caddy/Caddyfile`、宿主 80/443 |
| `.env.example` | `COMPOSE_PATH_SEPARATOR`、`COMPOSE_FILE`、端口、域名、`ACME_EMAIL`、资源数字、`BOOTSTRAP_ADMIN_EMAIL` 等非敏感项 |
| `secrets/*.txt.example` | 密钥模板；真实 `secrets/*.txt` gitignore |
| `deploy/deploy.sh` + `deploy/deploy.ps1` | 一键：检查 Docker、补齐 `.env`/secrets、加 `--profile`、`up`、等待 healthy、smoke |
| `deploy/smoke-check.*` | 宿主端口 /（可选）HTTPS 探活 |

---

## 3. 服务、网络与健康检查

**核心服务与默认宿主端口**（均可 `.env` 覆盖；沿用现网避让策略）

| 服务 | 宿主 → 容器 | 依赖 |
|------|-------------|------|
| mysql | `13306→3306` | — |
| portal | `18000→8000`，`19000→9000` | mysql healthy |
| gateway | `18088→8088` | portal healthy |
| web | `18080→80` | portal + gateway healthy |

**可选**

| Profile | 服务 | 说明 |
|---------|------|------|
| `neo4j` | neo4j | Bolt/HTTP 端口可配；命名卷 `neo4j_data` 持久化；portal 用 `depends_on.neo4j.required: false` + healthy；**仅当 profile 启用时**才等待 neo4j，避免默认栈被 profile 服务卡住 |
| `tls` | caddy | 对外 80/443 反代 `web:80`；内网直连端口默认仍映射，可用 `.env` 关闭 |

**网络**：单一 project bridge；服务 DNS 名互通。

**健康检查**

| 服务 | 探测 |
|------|------|
| mysql | `mysqladmin ping`；密码与 `MYSQL_ROOT_PASSWORD_FILE`（或等价从 `/run/secrets/mysql_root_password` 导出）对齐，**禁止** healthcheck 仍写死 `-proot` |
| gateway | 已有 `GET /healthz` |
| portal | **本迭代新增** `GET /readyz`（含 DB ping）作为 Compose `service_healthy` 探针；另可提供轻量 `GET /healthz`（进程活即可）。gateway/web 的 `depends_on` 等 portal **ready** |
| web | **必做** 静态 `GET /healthz` → 200（不依赖后端） |
| neo4j | 官方推荐 HTTP/cypher 探测 |
| caddy | 本地 HTTP 探测或等价 |

**TLS 边界**：`tls` profile 只终结 **Web UI**（Caddy → `web:80`）。Gateway 入站 / 企微长连接出站仍走映射的 HTTP `:18088`（或内网 DNS）；**本迭代不做** Gateway HTTPS 终结。

**重启与资源（默认，可 `.env` 覆盖）**

- 全部核心：`restart: unless-stopped`
- 内存上限约：mysql 1g、portal 1g、gateway 512m、web 128m、neo4j 1.5g、caddy 128m
- 不强制 CPU hard limit；文档说明可按机器再调

---

## 4. 密钥、配置与 bootstrap

**`.env`（非敏感）**  
端口、`COMPOSE_PROJECT_NAME`、`DOMAIN`、`ACME_EMAIL`、是否暴露内部端口、资源上限、`BOOTSTRAP_ADMIN_EMAIL`、Neo4j 用户名等。

**File secrets（`./secrets/*.txt`）**

| 文件 | 用途 |
|------|------|
| `mysql_root_password.txt` | MySQL root；拼装 portal DSN |
| `runtime_token.txt` | Gateway↔Portal Runtime（对齐 `SATH_RUNTIME_TOKEN` / `runtime.service_token`） |
| `bootstrap_password.txt` | `auth.bootstrap_password` |
| `bootstrap_token.txt` | `auth.bootstrap_token`（替换 `dev-bootstrap-token`） |
| `neo4j_password.txt` | 仅 `neo4j` profile |

**注入**

- Compose `secrets:` + `file:`（Windows Desktop 与 Linux 均可用，不依赖 Swarm）。
- 容器读 `/run/secrets/<name>`；portal/gateway 用 **entrypoint 包装脚本**（或现有 env 覆盖）在启动前注入，**不把密钥 bake 进镜像层**。
- `channels.yaml`：仅宿主机 bind-mount；真实企微凭证不进 git、不进镜像。
- **与「保留 `docker compose up`」的并存规则**：
  1. Git 只入库 `secrets/*.txt.example`，**不**入库真实 `*.txt`。
  2. 裸 `docker compose up` 与 deploy 脚本都依赖工作区已有 `secrets/*.txt`（Compose `file:` 指向这些路径）。
  3. 开发者首次：跑 `deploy` 脚本（自动复制 `.env.example` → `.env` 与 secrets examples），或手动复制；裸 `docker compose up` **同时需要** `.env`（含 `COMPOSE_FILE`）与 `secrets/*.txt`。
  4. README「简短 `docker compose up`」路径写明：先具备 `.env` + secrets，再 `up`。
  5. **不做**「默认栈用 env、secrets 仅生产 overlay」的双轨，以免 DSN/token 两套来源。
  6. 生产文档要求替换全部 secret 文件后再 `up`。

**Bootstrap 管理员**

- 复用 Portal 已有 `auth.bootstrap_email` + `auth.bootstrap_password`（幂等同步到 bootstrap 用户）。
- Compose/脚本：`BOOTSTRAP_ADMIN_EMAIL` + secret `bootstrap_password`。
- 文档标明：与现码一致为启动时幂等同步；生产应首次登录后改密；「仅无用户时创建」列为后续增强，本迭代不做行为变更。

**TLS（Caddy）**

- Let’s Encrypt 需公网 `DOMAIN` + `ACME_EMAIL`；无域名/无公网时不要开 `tls` profile。
- 默认路径保持 HTTP（`http://localhost:18080`）。

---

## 5. 一键脚本与命令矩阵

**CLI（行为对齐）**

```text
./deploy/deploy.sh [--with-neo4j] [--with-tls] [--build] [--down] [--smoke-only]
.\deploy\deploy.ps1 -WithNeo4j -WithTls -Build ...
```

步骤：检查 Docker → 补齐 `.env`/secrets → 组装 profile → `compose up -d` → 等待 healthy → smoke → 打印 URL 与管理员邮箱提示。

**Smoke**：探测 web `/healthz`、gateway `/healthz`、portal `/readyz`（必要时兼探 `/healthz`）；TLS 时可选探测 `https://$DOMAIN`。失败非 0 退出。

| 场景 | 命令 |
|------|------|
| 默认 HTTP | `./deploy/deploy.sh --build` |
| + Neo4j | `./deploy/deploy.sh --build --with-neo4j` |
| + TLS | `./deploy/deploy.sh --build --with-tls` |
| 全开 | `./deploy/deploy.sh --build --with-neo4j --with-tls` |
| 停栈保留卷 | `./deploy/deploy.sh --down` |
| 仅验活 | `./deploy/deploy.sh --smoke-only` |
| 纯 compose | `docker compose --profile neo4j --profile tls up -d --build` |

README 增加「生产 Compose」小节，指向本设计与脚本；保留现有简短 `docker compose up` 开发路径说明。

---

## 6. 实现触点（规划用）

1. **Compose**：演进 `docker-compose.yml`；新增 `compose.neo4j.yml`、`compose.tls.yml`、`.env.example`、`secrets/*`、gitignore。
2. **Portal**：新增 `/healthz` + `/readyz`（ready 含 DB）；docker entrypoint 从 secrets 读 mysql 密码、runtime token、bootstrap 字段并生成/覆盖 conf；neo4j profile 下图记忆 URI 指向 `neo4j://neo4j:7687`（具体 yaml 片段路径在实现计划中钉死到现有 memory/graph 配置挂载点）。
3. **Gateway**：entrypoint 从 secret 注入 `runtime_token`（若尚未支持）。
4. **Web**：nginx **必做** `/healthz`。
5. **Caddy**：`deploy/caddy/Caddyfile`（仅反代 web）。
6. **脚本**：`deploy/deploy.sh`、`deploy/deploy.ps1`、`deploy/smoke-check.*`；首次自动 `cp` secrets examples。
7. **文档**：根 README 命令矩阵；`secrets/README.md`；说明裸 `compose up` 前需具备 secrets 文件。

镜像构建上下文保持现状：portal/gateway 从 monorepo 根构建；web 从 `./web`。

**Compose 版本**：文档与脚本检查 Docker Compose **≥ 2.20**（支持 profile 服务上的 `depends_on.required: false`）。

**Portal conf 与 secrets**：现网只读挂载 `config.docker.yaml` 与 entrypoint 注入冲突时，**默认采用 (a)**：entrypoint 将挂载文件复制到可写路径再覆盖敏感字段（因现网仅见 `SATH_RUNTIME_TOKEN` 等有限 env 覆盖，bootstrap 邮箱/密码未必有等价 env）。若实现期确认运行时已支持全部所需 env，可改 (b)。**禁止**在只读挂载上原地写。

**数据卷**：保留现有 `mysql_data` + `deploy/mysql/init.sql`；`neo4j` profile 增加 `neo4j_data` 命名卷。

---

## 7. 验收标准

1. 清卷后脚本一键起栈，浏览器可打开 Web UI。
2. 使用 bootstrap 邮箱/密码可登录。
3. gateway `/healthz`、portal `/readyz`（及 `/healthz`）返回 200；核心服务 Compose 显示 healthy。
4. `--with-neo4j` / `--with-tls` 可独立开关，且不破坏默认 HTTP 路径。
5. 同一套文件在 Windows Docker Desktop 与 Linux 上均可跑通（端口与 secrets 路径用相对路径）。
6. 仓库中无真实生产密钥；仅 `*.example` 入库。

---

## 8. 非目标与后续

**本迭代不做**：自动备份/恢复 Job、Swarm/K8s、多副本 Gateway（企微长连接本就单连接）、把开发机绝对路径配置挂进生产。

**可后续**：bootstrap「仅空库创建一次」、secrets → 外部 Vault、备份 cron 示例 compose profile、镜像预构建 registry 推送流水线。
