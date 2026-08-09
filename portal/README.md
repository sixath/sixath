# Portal

Agent 平台后端服务，基于 [Kratos](https://go-kratos.dev/) 与 [sixath/framework](https://github.com/sixath/framework) 构建，提供工具管理、Agent 管理、对话会话与流式对话等能力。

## 功能概览

| 模块 | 说明 |
|------|------|
| **工具管理** | 内置工具、MCP 工具的 CRUD，支持 Agent 绑定 |
| **Agent 管理** | Agent CRUD、模型配置、Workspace、技能包上传、工具绑定 |
| **对话** | 会话管理、消息历史、流式 SSE 对话 |
| **技能** | 技能包校验与上传、解压到 `workspace/skills` |
| **记忆** | `MemoryStore` 门面：`memory_remember` / `memory_recall` / `memory_get`（session / agent / user）；Prefetch 围栏；见 [记忆使用指南](docs/memory-integration.md) |

## 技术栈

- **框架**：Go-Kratos v2
- **存储**：MySQL + GORM
- **Agent 引擎**：github.com/sixath/framework（ReActAgent、流式输出、MCP 工具）

## 快速开始

### 前置条件

- Go 1.25+
- MySQL 8.0+
- 已创建数据库（参考 `configs/config.yaml` 中的 `source`）

### 数据库初始化

创建数据库并执行建表 SQL（表结构见 [架构设计文档](docs/architecture_design.md) 三、MySQL 表设计）：

```sql
CREATE DATABASE sath CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
-- 执行 tools、agents、agent_tools、chat_sessions、chat_messages 等表的 DDL
```

### 配置

编辑 `configs/config.yaml`：

```yaml
server:
  http:
    addr: 0.0.0.0:8000
    timeout: 120s
  grpc:
    addr: 0.0.0.0:9000
    timeout: 30s
data:
  database:
    driver: mysql
    source: root:root@tcp(localhost:3306)/sath?parseTime=True&loc=Local&charset=utf8mb4
```

### 构建与运行

```bash
# 安装依赖
make init

# 生成 API 与 Wire
make all

# 构建
make build

# 运行（默认读取 configs 目录）
./bin/backend -conf ./configs
```

服务启动后：

- **HTTP**：`http://localhost:8000`
- **gRPC**：`localhost:9000`

## API 概览

### 工具

| 方法 | 路径 |
|------|------|
| POST | `/api/v1/tools` |
| GET | `/api/v1/tools` |
| GET | `/api/v1/tools/{id}` |
| PUT | `/api/v1/tools/{id}` |
| DELETE | `/api/v1/tools/{id}` |

### Agent

| 方法 | 路径 |
|------|------|
| POST | `/api/v1/agents` |
| GET | `/api/v1/agents` |
| GET | `/api/v1/agents/{id}` |
| PUT | `/api/v1/agents/{id}` |
| DELETE | `/api/v1/agents/{id}` |
| POST | `/api/v1/agents/{id}/tools` |
| DELETE | `/api/v1/agents/{id}/tools` |
| GET | `/api/v1/agents/{id}/skills` |
| POST | `/api/v1/agents/{id}/skills/validate` |
| POST | `/api/v1/agents/{id}/skills/upload` |
| POST | `/api/v1/agents/{id}/skills/execute` |
| DELETE | `/api/v1/agents/{id}/skills/{skill_name}` |

### 对话

| 方法 | 路径 |
|------|------|
| POST | `/api/v1/agents/{agent_id}/sessions` |
| GET | `/api/v1/agents/{agent_id}/sessions` |
| GET | `/api/v1/sessions/{id}` |
| PUT | `/api/v1/sessions/{id}` |
| DELETE | `/api/v1/sessions/{id}` |
| POST | `/api/v1/sessions/{session_id}/messages` |
| POST | `/api/v1/sessions/{session_id}/messages/stream` |
| GET | `/api/v1/sessions/{session_id}/messages` |

流式对话使用 SSE，请求 `POST /api/v1/sessions/{session_id}/messages/stream`，响应事件：`chunk`、`done`、`error`。

## 项目结构

```
portal/
├── api/                    # Proto 定义与生成代码
│   ├── agent/v1/
│   ├── chat/v1/
│   ├── tool/v1/
│   └── common/
├── cmd/backend/            # 入口与 Wire 注入
├── configs/                # 配置文件
├── internal/
│   ├── biz/               # 业务逻辑
│   ├── chat/              # Agent 构建（BuildModel、BuildRegistry、技能等）
│   ├── conf/              # 配置结构
│   ├── data/              # 数据访问（MySQL）
│   ├── server/            # HTTP/gRPC 服务、中间件、SSE
│   ├── service/           # 服务层
│   └── validator/         # 技能包校验
├── docs/
│   ├── architecture_design.md
│   └── requirement.md
└── third_party/            # Proto 依赖
```

## Makefile 命令

| 命令 | 说明 |
|------|------|
| `make init` | 安装 protoc、kratos、wire 等工具 |
| `make api` | 生成 pb.go、http、grpc、openapi |
| `make config` | 生成 internal proto |
| `make generate` | go generate + go mod tidy |
| `make all` | api + config + generate |
| `make build` | 编译到 `bin/` |

## Docker

单独构建 portal 镜像需在 **monorepo 根目录**执行（`go.mod` 通过 `replace` 引用 `../framework`）：

```bash
# 在仓库根目录
docker build -f portal/Dockerfile -t portal .
docker run --rm -p 8000:8000 -p 9000:9000 \
  -v "$(pwd)/portal/configs:/data/conf" portal
```

### Docker Compose（MySQL + portal + web）

完整本地栈在 **monorepo 根目录**（与 `web`、`framework` 同级）：

```bash
docker compose up --build -d
```

宿主端口已避开常见占用（见根目录 `docker-compose.yml`）：

| 服务 | 地址 |
|------|------|
| Web UI | http://localhost:18080（`/api` 经 nginx 反代到 portal） |
| Portal HTTP | http://localhost:18000 |
| Portal gRPC | localhost:19000 |
| MySQL | localhost:13306（root/root，库名 `sath`） |

Docker 环境配置见 [`configs/config.docker.yaml`](configs/config.docker.yaml)（DSN 指向服务名 `mysql`）。启动后 portal 会 `AutoMigrate` 建表。更完整的编排说明见 monorepo 根目录 [`README.md`](../README.md)。

## 文档

- [需求规格](docs/requirement.md)
- [架构设计与接口规范](docs/architecture_design.md)
