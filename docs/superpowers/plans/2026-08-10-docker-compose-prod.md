# Production Docker Compose One-Click Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地规格 [`2026-08-10-docker-compose-prod-design.md`](../specs/2026-08-10-docker-compose-prod-design.md)：分层 Compose + file secrets + 可选 neo4j/tls profile + bootstrap 管理员 + Win/Linux 一键脚本与冒烟验活。

**Architecture:** 核心栈仍在 `docker-compose.yml`；`compose.neo4j.yml` / `compose.tls.yml` 始终经 `COMPOSE_FILE`（`COMPOSE_PATH_SEPARATOR=|`）加载，可选服务用 `profiles`。密钥经 Docker `secrets:` → entrypoint 导出 `SATH_*` → Portal/Gateway 既有/新增 env enrich。Portal 新增 `/healthz` + `/readyz`；Web nginx `/healthz`；deploy 脚本负责补齐 `.env`/secrets、等待 healthy、smoke。

**Tech Stack:** Docker Compose ≥ 2.20、MySQL 8、可选 Neo4j / Caddy、Go（portal/gateway）、nginx（web）、bash + PowerShell 脚本。

**Note:** Do not commit unless the user asks（本计划内 Commit 步骤在用户授权提交时再执行；实现过程可先留本地改动）。

---

## File map

| Path | Responsibility |
|------|----------------|
| `portal/internal/server/middleware/auth.go` | Skip `/healthz`、`/readyz`（同 `/metrics`） |
| `portal/internal/server/health.go` | `GET /healthz`（活）、`GET /readyz`（DB ping） |
| `portal/internal/server/http.go` | 注册 health 路由；注入 pinger |
| `portal/internal/conf/auth_env.go`（新） | `SATH_BOOTSTRAP_*` 覆盖 `conf.Auth` |
| `portal/internal/conf/data_env.go`（新） | `SATH_MYSQL_DSN` 或密码拼 DSN 覆盖 `data.database.source` |
| `portal/cmd/backend/main.go` | 加载后调用 Enrich*FromEnv / EnrichAuthFromSecretsEnv |
| `portal/Dockerfile` + `deploy/portal/docker-entrypoint.sh` | secrets → env → `exec backend` |
| `gateway`（已有 `SATH_RUNTIME_TOKEN`） | 可选 entrypoint 从 secret 导出同名 env |
| `web/nginx.conf` | 静态 `location = /healthz` |
| `docker-compose.yml` | secrets、healthcheck、restart、limits、就绪依赖 |
| `compose.neo4j.yml` | neo4j profile + portal 覆盖 + `depends_on.required: false` |
| `compose.tls.yml` | caddy profile |
| `.env.example` | `COMPOSE_PATH_SEPARATOR`、`COMPOSE_FILE`、端口、bootstrap email |
| `secrets/*.txt.example` + `secrets/README.md` | 密钥模板 |
| `.gitignore` | `.env`、`secrets/*.txt`（保留 `*.example`） |
| `deploy/caddy/Caddyfile` | TLS 反代 web |
| `deploy/deploy.sh` + `deploy/deploy.ps1` | 一键 |
| `deploy/smoke-check.sh` + `deploy/smoke-check.ps1` | 验活 |
| `README.md` | 生产 Compose 命令矩阵 |

---

### Task 1: Portal auth skip + `/healthz` / `/readyz`

**Files:**
- Modify: `portal/internal/server/middleware/auth.go`
- Modify: `portal/internal/server/middleware/auth_test.go`
- Create: `portal/internal/server/health.go`
- Create: `portal/internal/server/health_test.go`
- Modify: `portal/internal/server/http.go`
- Modify: `portal/cmd/backend/wire.go` / `wire_gen.go`（若 NewHTTPServer 签名增加 pinger）
- Modify: `portal/internal/data/data.go`（导出 `Ping(ctx) error` 或提供 `DB() *gorm.DB`）

- [ ] **Step 1: Write failing auth skip tests**

在 `auth_test.go` 增加：

```go
func TestAuthSkipsHealthzAndReadyz(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		// same pattern as TestAuthSkipsMetricsPath — expect handler called without bearer
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `cd portal && go test ./internal/server/middleware/ -run TestAuthSkipsHealthz -v`  
Expected: FAIL（路径未 skip）

- [ ] **Step 3: Skip paths in Auth**

```go
if path == "/metrics" || path == "/healthz" || path == "/readyz" ||
```

- [ ] **Step 4: Write health handler tests**

```go
func TestHealthzOK(t *testing.T) { /* recorder → 200, body ok */ }
func TestReadyzOK(t *testing.T) { /* pinger nil err → 200 */ }
func TestReadyzFails(t *testing.T) { /* pinger err → 503 */ }
```

- [ ] **Step 5: Implement `health.go` + register on `Route("/")`**

`/healthz` → `200` text/plain `ok`  
`/readyz` → 调用 `Ping(ctx)`；失败 `503`

Pinger 来源：`Data` 增加：

```go
func (d *Data) Ping(ctx context.Context) error {
	sqlDB, err := d.db.DB()
	if err != nil { return err }
	return sqlDB.PingContext(ctx)
}
```

Wire：`NewHTTPServer` 增加 `pinger` 接口参数（或 `*data.Data`）。改完后：

Run: `cd portal && go generate ./cmd/backend/`（或项目惯用 wire 命令）  
然后：`go test ./internal/server/... ./internal/server/middleware/ -count=1`

- [ ] **Step 6: Commit（若用户已授权提交）**

```bash
git add portal/internal/server portal/internal/data portal/cmd/backend
git commit -m "feat(portal): add /healthz and /readyz for compose probes"
```

---

### Task 2: Portal env enrich for bootstrap + MySQL DSN

**Files:**
- Create: `portal/internal/conf/auth_env.go`
- Create: `portal/internal/conf/auth_env_test.go`
- Create: `portal/internal/conf/data_env.go`
- Create: `portal/internal/conf/data_env_test.go`
- Modify: `portal/cmd/backend/main.go`（在 conf 加载后、`NewData` 前 enrich）

- [ ] **Step 1: Failing tests for auth env**

```go
func TestEnrichAuthFromEnv(t *testing.T) {
	t.Setenv("SATH_BOOTSTRAP_EMAIL", "admin@example.com")
	t.Setenv("SATH_BOOTSTRAP_PASSWORD", "secret")
	t.Setenv("SATH_BOOTSTRAP_TOKEN", "tok")
	auth := &Auth{}
	EnrichAuthFromEnv(auth)
	if auth.GetBootstrapEmail() != "admin@example.com" { t.Fatal(...) }
	// password + token similarly
}
```

- [ ] **Step 2: Implement `EnrichAuthFromEnv`**

环境变量（非空才覆盖）：
- `SATH_BOOTSTRAP_EMAIL` → `bootstrap_email`
- `SATH_BOOTSTRAP_PASSWORD` → `bootstrap_password`
- `SATH_BOOTSTRAP_TOKEN` → `bootstrap_token`

函数签名：`func EnrichAuthFromEnv(auth **Auth)` 或返回 `*Auth`——**若传入 nil，必须 `new(Auth)`**。原因：`config.docker.yaml` 无 `auth:` 段，Kratos Scan 后 `bc.Auth == nil`，直接解引用会丢 bootstrap。

测试补充：`TestEnrichAuthFromEnv_nilAuthAllocates`。

- [ ] **Step 3: Failing tests for data DSN env**

```go
func TestEnrichDataFromEnv_fullDSN(t *testing.T) {
	t.Setenv("SATH_MYSQL_DSN", "root:x@tcp(mysql:3306)/sath?parseTime=True&loc=Local&charset=utf8mb4")
	// EnrichDataFromEnv on *Data → database.source replaced
}
```

- [ ] **Step 4: Implement `EnrichDataFromEnv`**

优先 `SATH_MYSQL_DSN`；否则若设 `SATH_MYSQL_PASSWORD`，在已有 source 上替换 `user:password@` 段（文档约定 docker 默认 user=`root`、host=`mysql:3306`、db=`sath`）。实现要有单元测试覆盖「仅替换密码」路径。

- [ ] **Step 5: Wire into `main.go`**

在 Kratos `c.Load()` / Scan 得到 `bc` 之后、`wireApp`/`NewData` 之前：

```go
if bc.Auth == nil {
	bc.Auth = &conf.Auth{}
}
conf.EnrichAuthFromEnv(bc.Auth)
conf.EnrichDataFromEnv(bc.Data)
```

（若采用 `EnrichAuthFromEnv(auth **Auth)`，则一行调用即可。）`EnrichRuntimeFromEnv` 已在 `LoadRuntimeFromConfigPath` 内。

另：在 `portal/configs/config.docker.yaml` **增加最小 `auth:` 段**（仅占位 id/org，无明文密码），避免其它代码路径假设 Auth 非 nil：

```yaml
auth:
  bootstrap_user_id: "bootstrap"
  bootstrap_org_id: "default"
  # email/password/token 由 SATH_BOOTSTRAP_* / secrets 注入
```

Run: `cd portal && go test ./internal/conf/ -count=1`

- [ ] **Step 6: Commit（若已授权）**

```bash
git commit -m "feat(portal): enrich auth and mysql DSN from SATH_* env"
```

---

### Task 3: Portal docker entrypoint（统一 conf-runtime + secrets→env）

**Files:**
- Create: `deploy/portal/docker-entrypoint.sh`
- Modify: `portal/Dockerfile`
- Compose（Task 6）将只读配置挂到 `/data/conf-ro`，可写运行目录 `/data/conf`（emptyDir/tmpfs 或匿名卷）

**钉死的 portal 配置路径（后续 Task 6/7 不得改回双轨）：**

1. 镜像 `WORKDIR /app`；`ENTRYPOINT` = 本脚本。
2. 启动时：`mkdir -p /data/conf && cp -a /data/conf-ro/. /data/conf/`（若 `/data/conf-ro` 存在）。
3. 从 `/run/secrets/*` 导出 `SATH_*`（见下）。
4. 若存在 `/run/secrets/neo4j_password` 且 `/data/conf/agent_extra.yaml` 存在：用 `sed`/`awk` 把 `neo4j.password`（或 `password: ""` 占位）写成 secret 内容（仅写 `/data/conf` 副本，不写只读源）。
5. `exec ./backend -conf /data/conf`

- [ ] **Step 1: Write entrypoint（完整伪代码，直接可落盘）**

```sh
#!/bin/sh
set -eu
if [ -d /data/conf-ro ]; then
  mkdir -p /data/conf
  cp -a /data/conf-ro/. /data/conf/
fi
read_secret() {
  f="/run/secrets/$1"
  if [ -f "$f" ]; then tr -d '\r\n' < "$f"; fi
}
[ -z "${SATH_RUNTIME_TOKEN:-}" ] && v=$(read_secret runtime_token) && [ -n "$v" ] && export SATH_RUNTIME_TOKEN="$v"
[ -z "${SATH_BOOTSTRAP_PASSWORD:-}" ] && v=$(read_secret bootstrap_password) && [ -n "$v" ] && export SATH_BOOTSTRAP_PASSWORD="$v"
[ -z "${SATH_BOOTSTRAP_TOKEN:-}" ] && v=$(read_secret bootstrap_token) && [ -n "$v" ] && export SATH_BOOTSTRAP_TOKEN="$v"
[ -z "${SATH_MYSQL_PASSWORD:-}" ] && v=$(read_secret mysql_root_password) && [ -n "$v" ] && export SATH_MYSQL_PASSWORD="$v"
if [ -z "${SATH_BOOTSTRAP_EMAIL:-}" ] && [ -n "${BOOTSTRAP_ADMIN_EMAIL:-}" ]; then
  export SATH_BOOTSTRAP_EMAIL="$BOOTSTRAP_ADMIN_EMAIL"
fi
NEO4J_PW=$(read_secret neo4j_password || true)
if [ -n "${NEO4J_PW:-}" ] && [ -f /data/conf/agent_extra.yaml ]; then
  awk -v pw="$NEO4J_PW" '{gsub(/REPLACE_ME/, pw); print}' /data/conf/agent_extra.yaml > /data/conf/agent_extra.yaml.tmp
  mv /data/conf/agent_extra.yaml.tmp /data/conf/agent_extra.yaml
fi
exec ./backend -conf /data/conf
```

密码注入以 `REPLACE_ME` + awk 为准（与 Task 7 的 `agent_extra.neo4j.yaml` 对齐）。

- [ ] **Step 2: Dockerfile**

```dockerfile
COPY deploy/portal/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh \
 && apt-get update && apt-get install -y --no-install-recommends curl \
 && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/app/docker-entrypoint.sh"]
```

（curl 供 compose healthcheck 使用。）

- [ ] **Step 3: `sh -n deploy/portal/docker-entrypoint.sh`**

Expected: exit 0

- [ ] **Step 4: Commit（若已授权）**

---

### Task 4: Web `/healthz`

**Files:**
- Modify: `web/nginx.conf`

- [ ] **Step 1: Add location**

```nginx
location = /healthz {
    add_header Content-Type text/plain;
    return 200 'ok';
}
```

放在 SPA `location /` **之前**。

- [ ] **Step 2: Commit（若已授权）**

---

### Task 5: Secrets templates + gitignore + `.env.example`

**Files:**
- Create: `secrets/README.md`
- Create: `secrets/mysql_root_password.txt.example`（内容一行：`root`）
- Create: `secrets/runtime_token.txt.example`（`dev-runtime-token`）
- Create: `secrets/bootstrap_password.txt.example`（`change-me`）
- Create: `secrets/bootstrap_token.txt.example`（`dev-bootstrap-token`）
- Create: `secrets/neo4j_password.txt.example`（`changeme-neo4j`）
- Create: `.env.example`
- Modify: `.gitignore`

- [ ] **Step 1: `.env.example` 关键项**

```env
COMPOSE_PATH_SEPARATOR=|
COMPOSE_FILE=docker-compose.yml|compose.neo4j.yml|compose.tls.yml
COMPOSE_PROJECT_NAME=sixath

WEB_HOST_PORT=18080
GATEWAY_HOST_PORT=18088
PORTAL_HTTP_HOST_PORT=18000
PORTAL_GRPC_HOST_PORT=19000
MYSQL_HOST_PORT=13306

BOOTSTRAP_ADMIN_EMAIL=admin@example.com

# TLS profile
DOMAIN=
ACME_EMAIL=

# optional resource (bytes) — used by compose deploy.resources if wired
# MYSQL_MEM_LIMIT=1073741824
```

- [ ] **Step 2: `.gitignore` 增加**

```
.env
secrets/*.txt
!secrets/*.txt.example
!secrets/README.md
deploy/last-smoke.json
```

- [ ] **Step 3: Commit（若已授权）**

---

### Task 6: Evolve `docker-compose.yml`（核心栈）

**Files:**
- Modify: `docker-compose.yml`
- Modify: `portal/configs/config.docker.yaml`（保留 DSN 默认；Task 2 已加 `auth:`）
- Create: `deploy/gateway/docker-entrypoint.sh`
- Modify: `gateway/Dockerfile`（curl + entrypoint 导出 `SATH_RUNTIME_TOKEN`）

**必须保留（勿在重写时丢掉）：**
- volume `mysql_data` + `./deploy/mysql/init.sql` 挂载
- gateway：`./gateway/configs/config.docker.yaml` + `./gateway/configs/channels.yaml` bind-mount
- portal build context = monorepo 根

- [ ] **Step 1: Top-level secrets**

```yaml
secrets:
  mysql_root_password:
    file: ./secrets/mysql_root_password.txt
  runtime_token:
    file: ./secrets/runtime_token.txt
  bootstrap_password:
    file: ./secrets/bootstrap_password.txt
  bootstrap_token:
    file: ./secrets/bootstrap_token.txt
```

- [ ] **Step 2: mysql service**

- `MYSQL_ROOT_PASSWORD_FILE: /run/secrets/mysql_root_password`
- `secrets: [mysql_root_password]`
- `restart: unless-stopped`
- `mem_limit: 1g`
- healthcheck：

```yaml
healthcheck:
  test: ["CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -uroot -p$$(cat /run/secrets/mysql_root_password) || exit 1"]
  interval: 5s
  timeout: 5s
  retries: 20
  start_period: 25s
```

- ports: `"${MYSQL_HOST_PORT:-13306}:3306"`
- 保留 `mysql_data` 与 `init.sql`

- [ ] **Step 3: portal service**

```yaml
volumes:
  - ./portal/configs/config.docker.yaml:/data/conf-ro/config.yaml:ro
environment:
  BOOTSTRAP_ADMIN_EMAIL: ${BOOTSTRAP_ADMIN_EMAIL:-admin@example.com}
secrets: [mysql_root_password, runtime_token, bootstrap_password, bootstrap_token]
tmpfs:
  - /data/conf:mode=1777
healthcheck:
  test: ["CMD-SHELL", "curl -fsS http://127.0.0.1:8000/readyz || exit 1"]
  interval: 5s
  timeout: 5s
  retries: 30
  start_period: 40s
depends_on:
  mysql:
    condition: service_healthy
restart: unless-stopped
mem_limit: 1g
ports:
  - "${PORTAL_HTTP_HOST_PORT:-18000}:8000"
  - "${PORTAL_GRPC_HOST_PORT:-19000}:9000"
```

（entrypoint 按 Task 3 从 `/data/conf-ro` → `/data/conf`。）

- [ ] **Step 4: gateway service**

- **必须** `secrets: [runtime_token]`
- **删除** 现有（若有）`environment.SATH_RUNTIME_TOKEN: dev-runtime-token` 硬编码——token **只**经 secret → entrypoint → `SATH_RUNTIME_TOKEN`；config.docker.yaml 里可留占位，env 覆盖为准
- entrypoint：`export SATH_RUNTIME_TOKEN=$(tr -d '\\r\\n' < /run/secrets/runtime_token)` 后 `exec ./gateway ...`
- 保留 config + channels 挂载
- Dockerfile 安装 curl
- healthcheck: `curl -fsS http://127.0.0.1:8088/healthz`
- `depends_on.portal.condition: service_healthy`
- `restart: unless-stopped`
- `mem_limit: 512m`
- ports: `"${GATEWAY_HOST_PORT:-18088}:8088"`

- [ ] **Step 5: web service**

```yaml
healthcheck:
  test: ["CMD-SHELL", "wget -qO- http://127.0.0.1/healthz | grep -q ok"]
depends_on:
  portal:
    condition: service_healthy
  gateway:
    condition: service_healthy
restart: unless-stopped
mem_limit: 128m
ports:
  - "${WEB_HOST_PORT:-18080}:80"
```

- [ ] **Step 6: `docker compose config` 静态校验**（先具备 `.env` + `secrets/*.txt`）

Expected: 成功渲染。

- [ ] **Step 7: Commit（若已授权）**

---

### Task 7: `compose.neo4j.yml`

**Files:**
- Create: `compose.neo4j.yml`
- Create: `deploy/portal/agent_extra.neo4j.yaml`
- Create: `deploy/neo4j/docker-entrypoint.sh`（仅设置 `NEO4J_AUTH`）

**钉死策略（不再摇摆）：**
1. Neo4j 容器：自定义 entrypoint `export NEO4J_AUTH="neo4j/$(tr -d '\\r\\n' < /run/secrets/neo4j_password)"` 后 `exec /startup/docker-entrypoint.sh neo4j`（或镜像默认入口）。**不使用** `NEO4J_AUTH_FILE`（避免镜像版本差异）。
2. Portal 图配置：`compose.neo4j.yml` 向 portal 追加只读挂载  
   `./deploy/portal/agent_extra.neo4j.yaml:/data/conf-ro/agent_extra.yaml:ro`  
   密码占位为 `password: "REPLACE_ME"`；Task 3 entrypoint 在复制到 `/data/conf` 后若存在 neo4j secret，则把 `REPLACE_ME` 替换为真实密码。
3. portal `depends_on.neo4j`：`condition: service_healthy` + **`required: false`**。
4. 默认栈不加 neo4j secret 到 portal（仅 profile 文件声明 `neo4j_password`；启用 profile 时 portal 才能读到该 secret——在 `compose.neo4j.yml` 的 `portal.secrets` 追加）。

- [ ] **Step 1: 写 `agent_extra.neo4j.yaml`**

```yaml
memory_store:
  graph:
    enabled: true
    provider: neo4j
    min_relation_confidence: 0.7
    max_hops: 1
    rrf_k: 60
    neo4j:
      uri: bolt://neo4j:7687
      username: neo4j
      password: "REPLACE_ME"
      database: ""
```

- [ ] **Step 2: 写 neo4j entrypoint + `compose.neo4j.yml` 全文**

含：`profiles: ["neo4j"]`、ports 可配、`neo4j_data` 卷、`mem_limit: 1536m`、healthcheck（`wget -qO- http://127.0.0.1:7474` 或官方推荐）、`secrets.neo4j_password`、portal 覆盖块。

- [ ] **Step 3: 校验**

```bash
docker compose config >/dev/null
docker compose --profile neo4j config >/dev/null
```

Expected: 无 profile 时服务列表无 neo4j；有 profile 时有 neo4j，且 portal depends_on 含 required false。

- [ ] **Step 4: Commit（若已授权）**

---

### Task 8: `compose.tls.yml` + Caddyfile

**Files:**
- Create: `compose.tls.yml`
- Create: `deploy/caddy/Caddyfile`

- [ ] **Step 1: Caddyfile**

```
{$DOMAIN} {
  reverse_proxy web:80
}
```

- [ ] **Step 2: compose.tls.yml**

```yaml
services:
  caddy:
    profiles: ["tls"]
    image: caddy:2
    ports:
      - "80:80"
      - "443:443"
    environment:
      DOMAIN: ${DOMAIN:-localhost}
      ACME_EMAIL: ${ACME_EMAIL:-admin@example.com}
    volumes:
      - ./deploy/caddy/Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    depends_on:
      web:
        condition: service_healthy
    restart: unless-stopped
    mem_limit: 128m
    healthcheck:
      test: ["CMD", "caddy", "version"]
      interval: 30s
      timeout: 5s
      retries: 3

volumes:
  caddy_data:
  caddy_config:
```

Caddyfile 增加全局邮箱（若支持）：

```
{
  email {$ACME_EMAIL}
}
{$DOMAIN} {
  reverse_proxy web:80
}
```

文档：无公网域名勿开 profile。TLS **只**终结 Web UI。  
**变量规则：** 禁止 `${DOMAIN:?...}`（`COMPOSE_FILE` 始终加载本文件，空 DOMAIN 会弄挂默认 HTTP `compose config`）。deploy 脚本在 `--with-tls` 时校验 `DOMAIN` 非空且非 `localhost`。

- [ ] **Step 3: Commit（若已授权）**

---

### Task 9: Deploy + smoke scripts（bash + PowerShell）

**Files:**
- Create: `deploy/deploy.sh`
- Create: `deploy/deploy.ps1`
- Create: `deploy/smoke-check.sh`
- Create: `deploy/smoke-check.ps1`

- [ ] **Step 1: 公共行为清单（两脚本对齐）**

1. 检查 `docker compose version` ≥ 2.20（解析版本号）
2. 若无 `.env`：`cp .env.example .env`
3. 对每个 `secrets/*.txt.example`：若缺对应 `.txt`，复制并打印 WARNING
4. 解析参数：`--with-neo4j` / `--with-tls` / `--build` / `--down` / `--smoke-only`  
   - 若 `--with-tls`：要求 `.env` 中 `DOMAIN` 非空且不是 `localhost`，否则 exit 1 并提示
5. `docker compose [--profile neo4j] [--profile tls] up -d [--build]`
6. 等待：循环 `docker compose ps` / curl host ports 直至 ready 或超时（例如 180s）
7. 调用 smoke-check
8. 打印 URL：`http://127.0.0.1:${WEB_HOST_PORT}` 与 bootstrap email

`--down`：`docker compose --profile neo4j --profile tls down`（保留卷）

- [ ] **Step 2: smoke-check**

探测：
- `GET http://127.0.0.1:$WEB_HOST_PORT/healthz`
- `GET http://127.0.0.1:$GATEWAY_HOST_PORT/healthz`
- `GET http://127.0.0.1:$PORTAL_HTTP_HOST_PORT/readyz`

失败 exit 1。可选写 `deploy/last-smoke.json`。

- [ ] **Step 3: PowerShell 对等实现**（参数 `-WithNeo4j` 等）

- [ ] **Step 4: 干跑**

```powershell
# 不 up 全栈也可先测帮助
.\deploy\deploy.ps1 -?
```

- [ ] **Step 5: Commit（若已授权）**

---

### Task 10: README + 端到端验收

**Files:**
- Modify: `README.md`（「快速开始」旁增加「生产 Compose / 一键部署」）

- [ ] **Step 1: 文档段落**

写入命令矩阵（与规格 §5 一致）、secrets 说明、Compose ≥ 2.20、`COMPOSE_PATH_SEPARATOR`、TLS 边界、bootstrap 登录提示。

- [ ] **Step 2: E2E（本机 Docker）**

```powershell
.\deploy\deploy.ps1 -Build
.\deploy\deploy.ps1 -SmokeOnly
```

Expected: 全部 healthy；浏览器打开 `http://127.0.0.1:18080`；可用 `BOOTSTRAP_ADMIN_EMAIL` + `bootstrap_password.txt` 登录。

可选：

```powershell
.\deploy\deploy.ps1 -Build -WithNeo4j
```

TLS 仅在有 `DOMAIN` 的机器验证。

- [ ] **Step 3: 回归默认路径**

无 profile 时 `docker compose ps` 不应出现 neo4j/caddy。

- [ ] **Step 4: Commit（若已授权）**

```bash
git commit -m "docs: document production compose one-click deploy"
```

---

## Acceptance mapping（规格 §7）

| 规格验收 | 覆盖 Task |
|----------|-----------|
| 清卷一键起 + Web UI | 6–10 |
| bootstrap 登录 | 2–3, 5–6, 10 |
| healthz/readyz + healthy | 1, 4, 6, 9 |
| neo4j/tls 独立开关 | 7–9 |
| Win + Linux 同一套文件 | 5, 9 |
| 无真实密钥入库 | 5 |

---

## Out of scope（勿实现）

自动备份 Job、Swarm/K8s、Gateway HTTPS、bootstrap「仅空库创建一次」行为变更、绑定 `E:\configs\sixath`。
