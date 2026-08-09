# Agent 平台架构设计与接口规范

> 本文档基于 [sath](https://github.com/jwcjlu/sath) 项目现有架构，给出技能/工具/Agent 管理的具体实现方案与 REST API 设计。sath 为 Go 语言 AI Agent 框架，已具备：`skills.Index`、`tool.Registry`、`agent.Agent`、`tool.McpConfig`、`execute_skill_script` 等能力。

## 一、sath 现有能力映射

| 需求模块 | sath 现有能力 | 待扩展 |
|----------|---------------|--------|
| **技能** | `skills.Index`（SKILL.md 扫描）、`LoadSkillBody`、`LoadSkillFile`、`execute_skill_script`（.sh/.py/.js） | 技能包上传、校验、解压；技能列表（扫描 workspace/skills）；技能删除 |
| **工具** | `tool.Registry`、`tool.Tool`、`RegisterMcpTool`、`McpConfig`、内置工具（calculator、list_tables 等） | 工具 CRUD 持久化、工具类型区分、Agent 绑定关系 |
| **Agent** | `agent.Agent`、`ChatAgent`、`ReActAgent`、`PlanAgent`、`model.Model` | Agent CRUD 持久化、Workspace、技能包上传、工具绑定、配置化构建 |
| **对话** | `ChatAgent`、`ReActAgent`、流式输出、工具调用 | 会话/消息持久化、会话管理、Chat API、流式 SSE |
| **执行** | `execute_skill_script`（sh/python/node）、`executor.Executor`（SQL/DSL） | 脚本语法校验、沙箱、环境变量注入 |
| **存储** | `metadata.Store`（Schema）、`datasource.Registry` | 新增 ToolStore、AgentStore、ChatSessionStore、ChatMessageStore、ChannelStore、CronTaskStore；技能存于 Agent workspace 文件系统 |
| **渠道** | 无 | Channel 抽象（web/api/webhook）、路由规则、访问控制 |
| **定时任务** | 无 | Cron 调度器、任务 CRUD、执行历史、结果投递（webhook/session） |

## 二、数据模型设计

### 2.1 技能包上传（Skill Package）

技能通过压缩包上传，校验通过后解压到 Agent 的 `workspace/skills` 目录，不单独建表。

```go
// api/agent/skill_upload.go
type SkillPackageUpload struct {
    // multipart/form-data: file 字段为 .zip 压缩包
}

type SkillPackageValidateResult struct {
    Valid   bool     `json:"valid"`
    Message string   `json:"message,omitempty"`
    Errors  []string `json:"errors,omitempty"`  // 校验失败项
}
```

**校验项**：压缩包格式、必需文件（SKILL.md）、脚本扩展名（.sh/.py/.js）、无路径逃逸（`../`）。

### 2.2 工具（Tool）

```go
// api/tool/types.go
type ToolType string
const (
    ToolTypeBuiltin ToolType = "builtin"
    ToolTypeMCP     ToolType = "mcp"
)

type ToolCreate struct {
    Name        string      `json:"name" binding:"required"`
    Description string      `json:"description" binding:"required"`
    Type        ToolType    `json:"type" binding:"required"`
    Config      ToolConfig  `json:"config" binding:"required"`
}

type ToolConfig struct {
    // 内置工具
    FuncPath   string      `json:"func_path,omitempty"`
    Parameters any         `json:"parameters,omitempty"` // JSON Schema
    Async      bool        `json:"async,omitempty"`
    // MCP 工具
    McpServerID string    `json:"mcp_server_id,omitempty"`
    McpEndpoint string    `json:"mcp_endpoint,omitempty"`
    McpBackend  string    `json:"mcp_backend,omitempty"`  // metoro|mark3labs
    TimeoutSec  int       `json:"timeout_sec,omitempty"`
}

type ToolMeta struct {
    ID          string     `json:"id"`
    Name        string     `json:"name"`
    Description string     `json:"description"`
    Type        ToolType   `json:"type"`
    Config      ToolConfig `json:"config"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt    time.Time  `json:"updated_at"`
}
```

### 2.3 Agent

```go
// api/agent/types.go
type AgentCreate struct {
    Name          string       `json:"name" binding:"required"`
    Description   string       `json:"description,omitempty"`
    SystemPrompt  string       `json:"system_prompt,omitempty"`
    ModelConfig   ModelConfig  `json:"model_config" binding:"required"`
    Workspace     string       `json:"workspace" binding:"required"`  // 工作空间路径，技能解压到 workspace/skills
    ToolIDs       []string     `json:"tool_ids,omitempty"`
}

type ModelConfig struct {
    Provider string  `json:"provider"`  // openai|claude|qwen|...
    Model    string  `json:"model"`
    APIKey   string  `json:"api_key,omitempty"` // 加密存储
    BaseURL  string  `json:"base_url,omitempty"`
}

type AgentMeta struct {
    ID           string       `json:"id"`
    Name         string       `json:"name"`
    Description  string       `json:"description"`
    SystemPrompt string       `json:"system_prompt,omitempty"`
    ModelConfig  ModelConfig  `json:"model_config"`
    Workspace    string       `json:"workspace"`   // 工作空间路径
    ToolIDs      []string     `json:"tool_ids"`
    CreatedAt    time.Time    `json:"created_at"`
    UpdatedAt    time.Time    `json:"updated_at"`
}
```

### 2.4 对话会话与消息（Chat Session & Message）

```go
// api/chat/types.go
type ChatSessionCreate struct {
    AgentID string `json:"agent_id" binding:"required"`  // 关联的 Agent
    Title   string `json:"title,omitempty"`              // 会话标题，可选，默认 "新对话"
}

type ChatSessionMeta struct {
    ID        string    `json:"id"`
    AgentID   string    `json:"agent_id"`
    Title     string    `json:"title"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type ChatMessageRole string
const (
    RoleUser      ChatMessageRole = "user"
    RoleAssistant ChatMessageRole = "assistant"
    RoleSystem    ChatMessageRole = "system"
)

type ChatMessageCreate struct {
    SessionID string `json:"-"`                         // 由路径注入
    Role      string `json:"-"`                        // user/assistant，由调用方设置
    Content   string `json:"content" binding:"required"` // 用户消息内容（API 请求体仅含此字段）
}

type ChatMessageMeta struct {
    ID        string          `json:"id"`
    SessionID string          `json:"session_id"`
    Role      ChatMessageRole `json:"role"`
    Content   string          `json:"content"`
    CreatedAt time.Time       `json:"created_at"`
}

// 2.5 消息渠道（Channel）
// api/channel/types.go
type ChannelType string
const (
    ChannelWeb     ChannelType = "web"
    ChannelAPI     ChannelType = "api"
    ChannelWebhook ChannelType = "webhook"
)

type ChannelMeta struct {
    ID           string      `json:"id"`
    ChannelID    string      `json:"channel_id"`    // 唯一标识，如 web、api、webhook:dingtalk
    Type         ChannelType `json:"type"`
    DefaultAgent string      `json:"default_agent"`  // 未显式指定时的默认 Agent ID
    Enabled      bool        `json:"enabled"`
    // Webhook 额外配置
    WebhookPath   string   `json:"webhook_path,omitempty"`   // 回调 URL 路径
    WebhookSecret string   `json:"webhook_secret,omitempty"` // 签名密钥
    IPWhitelist   []string `json:"ip_whitelist,omitempty"`  // IP 白名单
    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
}

// 2.6 定时任务（Cron Task）
// api/cron/types.go
type ScheduleKind string
const (
    ScheduleCron ScheduleKind = "cron"
    ScheduleEvery ScheduleKind = "every"
    ScheduleAt   ScheduleKind = "at"
)

type PayloadKind string
const (
    PayloadAgentTurn   PayloadKind = "agent_turn"
    PayloadSkillExecute PayloadKind = "skill_execute"
)

type DeliveryMode string
const (
    DeliveryNone    DeliveryMode = "none"
    DeliveryWebhook DeliveryMode = "webhook"
    DeliverySession DeliveryMode = "session"
)

type CronTaskMeta struct {
    ID            string       `json:"id"`
    Name          string       `json:"name"`
    AgentID       string       `json:"agent_id"`
    ScheduleKind  ScheduleKind `json:"schedule_kind"`
    ScheduleExpr  string       `json:"schedule_expr"`  // cron 表达式 / 间隔秒数 / ISO 时间戳
    Timezone      string       `json:"timezone"`       // IANA 时区
    StaggerSec    int          `json:"stagger_sec"`    // 错峰秒数
    PayloadKind   PayloadKind  `json:"payload_kind"`
    PayloadContent string      `json:"payload_content"` // 消息内容或技能路径
    TimeoutSec    int          `json:"timeout_sec"`
    RetryCount    int          `json:"retry_count"`
    RetryIntervalSec int       `json:"retry_interval_sec"`
    Delivery      *DeliveryConfig `json:"delivery,omitempty"`
    Enabled       bool        `json:"enabled"`
    CreatedAt     time.Time   `json:"created_at"`
    UpdatedAt     time.Time   `json:"updated_at"`
}

type DeliveryConfig struct {
    Mode       DeliveryMode `json:"mode"`
    WebhookURL string       `json:"webhook_url,omitempty"`
    Secret     string       `json:"secret,omitempty"`
    BestEffort bool         `json:"best_effort"`
    SessionID  string       `json:"session_id,omitempty"`
}

type CronRunMeta struct {
    ID           string    `json:"id"`
    TaskID       string    `json:"task_id"`
    TriggeredAt  time.Time `json:"triggered_at"`
    StartedAt    time.Time `json:"started_at"`
    FinishedAt   *time.Time `json:"finished_at,omitempty"`
    Status       string    `json:"status"` // success / failed / timeout / cancelled
    OutputSummary string   `json:"output_summary,omitempty"`
    Error        string    `json:"error,omitempty"`
    DeliveryOK   *bool     `json:"delivery_ok,omitempty"` // 投递是否成功
}

// 流式响应：SSE 事件
type ChatStreamChunk struct {
    Content string `json:"content,omitempty"`  // 增量文本
    Done    bool   `json:"done,omitempty"`     // 是否结束
    Error   string `json:"error,omitempty"`    // 错误信息
}
```

## 三、MySQL 表设计

存储采用 MySQL，字符集 `utf8mb4`，引擎 InnoDB。

### 3.1 技能存储说明

技能不单独建表，通过上传压缩包解压到 Agent 的 `{workspace}/skills/` 目录下，由 `skills.Index` 扫描该目录加载。技能发现：遍历 `{workspace}/skills/` 子目录，含 SKILL.md 视为有效；技能删除：移除对应子目录 `{workspace}/skills/{skill_name}/`。

### 3.2 工具表 `tools`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| name | VARCHAR(128) | UNIQUE NOT NULL | 工具名称 |
| description | TEXT | NOT NULL | 工具描述 |
| type | VARCHAR(16) | NOT NULL | builtin / mcp |
| config | JSON | NOT NULL | 工具配置（FuncPath/Parameters/Async 或 McpServerID/McpEndpoint/McpBackend/TimeoutSec） |
| created_at | DATETIME(3) | NOT NULL | 创建时间 |
| updated_at | DATETIME(3) | NOT NULL | 更新时间 |

```sql
CREATE TABLE tools (
    id          VARCHAR(36)  NOT NULL PRIMARY KEY,
    name        VARCHAR(128) NOT NULL UNIQUE,
    description TEXT         NOT NULL,
    type        VARCHAR(16)  NOT NULL,
    config      JSON         NOT NULL,
    created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_tools_type (type),
    INDEX idx_tools_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.3 Agent 表 `agents`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| name | VARCHAR(128) | UNIQUE NOT NULL | Agent 名称 |
| description | TEXT | | 用途说明 |
| system_prompt | TEXT | | 系统提示词 |
| model_config | JSON | NOT NULL | 模型配置 `{"provider","model","api_key","base_url"}`，api_key 建议加密后存储 |
| workspace | VARCHAR(512) | NOT NULL | 工作空间路径，技能解压到 `{workspace}/skills/` |
| created_at | DATETIME(3) | NOT NULL | 创建时间 |
| updated_at | DATETIME(3) | NOT NULL | 更新时间 |

```sql
CREATE TABLE agents (
    id            VARCHAR(36)  NOT NULL PRIMARY KEY,
    name          VARCHAR(128) NOT NULL UNIQUE,
    description   TEXT,
    system_prompt TEXT,
    model_config  JSON         NOT NULL,
    workspace     VARCHAR(512) NOT NULL,
    created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_agents_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.4 Agent-工具绑定表 `agent_tools`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| agent_id | VARCHAR(36) | NOT NULL, FK→agents(id) | Agent ID |
| tool_id | VARCHAR(36) | NOT NULL, FK→tools(id) | 工具 ID |
| sort_order | INT | DEFAULT 0 | 优先级/执行顺序 |
| created_at | DATETIME(3) | NOT NULL | 绑定时间 |

```sql
CREATE TABLE agent_tools (
    agent_id    VARCHAR(36)  NOT NULL,
    tool_id     VARCHAR(36)  NOT NULL,
    sort_order  INT          DEFAULT 0,
    created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (agent_id, tool_id),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (tool_id)  REFERENCES tools(id)  ON DELETE CASCADE,
    INDEX idx_agent_tools_tool_id (tool_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.5 对话会话表 `chat_sessions`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| agent_id | VARCHAR(36) | NOT NULL, FK→agents(id) | 关联 Agent |
| title | VARCHAR(256) | DEFAULT '新对话' | 会话标题 |
| created_at | DATETIME(3) | NOT NULL | 创建时间 |
| updated_at | DATETIME(3) | NOT NULL | 更新时间 |

```sql
CREATE TABLE chat_sessions (
    id         VARCHAR(36)  NOT NULL PRIMARY KEY,
    agent_id   VARCHAR(36)  NOT NULL,
    title      VARCHAR(256)  NOT NULL DEFAULT '新对话',
    created_at DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    INDEX idx_chat_sessions_agent_id (agent_id),
    INDEX idx_chat_sessions_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.6 对话消息表 `chat_messages`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| session_id | VARCHAR(36) | NOT NULL, FK→chat_sessions(id) | 所属会话 |
| role | VARCHAR(16) | NOT NULL | user / assistant / system |
| content | TEXT | NOT NULL | 消息内容 |
| created_at | DATETIME(3) | NOT NULL | 创建时间 |

```sql
CREATE TABLE chat_messages (
    id         VARCHAR(36)  NOT NULL PRIMARY KEY,
    session_id VARCHAR(36)  NOT NULL,
    role       VARCHAR(16)  NOT NULL,
    content    TEXT         NOT NULL,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE,
    INDEX idx_chat_messages_session_id (session_id),
    INDEX idx_chat_messages_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.7 消息渠道表 `channels`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| channel_id | VARCHAR(64) | UNIQUE NOT NULL | 渠道标识，如 web、api、webhook:dingtalk |
| type | VARCHAR(16) | NOT NULL | web / api / webhook |
| default_agent | VARCHAR(36) | FK→agents(id) | 默认 Agent ID |
| enabled | TINYINT(1) | NOT NULL DEFAULT 1 | 是否启用 |
| webhook_path | VARCHAR(256) | | Webhook 回调路径 |
| webhook_secret | VARCHAR(256) | | 签名密钥（加密存储） |
| ip_whitelist | JSON | | IP 白名单 ["1.2.3.4","10.0.0.0/8"] |
| created_at | DATETIME(3) | NOT NULL | 创建时间 |
| updated_at | DATETIME(3) | NOT NULL | 更新时间 |

```sql
CREATE TABLE channels (
    id             VARCHAR(36)   NOT NULL PRIMARY KEY,
    channel_id     VARCHAR(64)   NOT NULL UNIQUE,
    type           VARCHAR(16)   NOT NULL,
    default_agent  VARCHAR(36),
    enabled        TINYINT(1)    NOT NULL DEFAULT 1,
    webhook_path   VARCHAR(256),
    webhook_secret VARCHAR(256),
    ip_whitelist   JSON,
    created_at     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    FOREIGN KEY (default_agent) REFERENCES agents(id) ON DELETE SET NULL,
    INDEX idx_channels_type (type),
    INDEX idx_channels_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.8 定时任务表 `cron_tasks`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| name | VARCHAR(128) | NOT NULL | 任务名称 |
| agent_id | VARCHAR(36) | NOT NULL, FK→agents(id) | 关联 Agent |
| schedule_kind | VARCHAR(16) | NOT NULL | cron / every / at |
| schedule_expr | VARCHAR(256) | NOT NULL | cron 表达式 / 间隔秒数 / ISO 时间戳 |
| timezone | VARCHAR(64) | DEFAULT 'UTC' | IANA 时区 |
| stagger_sec | INT | DEFAULT 0 | 错峰秒数 |
| payload_kind | VARCHAR(16) | NOT NULL | agent_turn / skill_execute |
| payload_content | TEXT | NOT NULL | 消息内容或技能路径 |
| timeout_sec | INT | DEFAULT 300 | 超时秒数 |
| retry_count | INT | DEFAULT 0 | 失败重试次数 |
| retry_interval_sec | INT | DEFAULT 60 | 重试间隔秒数 |
| delivery_mode | VARCHAR(16) | DEFAULT 'none' | none / webhook / session |
| delivery_webhook_url | VARCHAR(512) | | Webhook 投递 URL |
| delivery_secret | VARCHAR(256) | | 投递签名密钥 |
| delivery_best_effort | TINYINT(1) | DEFAULT 0 | 投递失败不视为任务失败 |
| delivery_session_id | VARCHAR(36) | FK→chat_sessions(id) | Session 投递目标 |
| enabled | TINYINT(1) | NOT NULL DEFAULT 1 | 是否启用 |
| next_run_at | DATETIME(3) | | 下次计划执行时间 |
| created_at | DATETIME(3) | NOT NULL | 创建时间 |
| updated_at | DATETIME(3) | NOT NULL | 更新时间 |

```sql
CREATE TABLE cron_tasks (
    id                    VARCHAR(36)   NOT NULL PRIMARY KEY,
    name                  VARCHAR(128)  NOT NULL,
    agent_id              VARCHAR(36)   NOT NULL,
    schedule_kind         VARCHAR(16)   NOT NULL,
    schedule_expr         VARCHAR(256) NOT NULL,
    timezone              VARCHAR(64)   NOT NULL DEFAULT 'UTC',
    stagger_sec            INT           NOT NULL DEFAULT 0,
    payload_kind          VARCHAR(16)   NOT NULL,
    payload_content       TEXT          NOT NULL,
    timeout_sec            INT           NOT NULL DEFAULT 300,
    retry_count            INT           NOT NULL DEFAULT 0,
    retry_interval_sec     INT           NOT NULL DEFAULT 60,
    delivery_mode          VARCHAR(16)   NOT NULL DEFAULT 'none',
    delivery_webhook_url   VARCHAR(512),
    delivery_secret        VARCHAR(256),
    delivery_best_effort   TINYINT(1)    NOT NULL DEFAULT 0,
    delivery_session_id    VARCHAR(36),
    enabled                TINYINT(1)    NOT NULL DEFAULT 1,
    next_run_at            DATETIME(3),
    created_at             DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at             DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
    FOREIGN KEY (delivery_session_id) REFERENCES chat_sessions(id) ON DELETE SET NULL,
    INDEX idx_cron_tasks_agent_id (agent_id),
    INDEX idx_cron_tasks_enabled_next (enabled, next_run_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.9 定时任务执行历史表 `cron_run_history`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | VARCHAR(36) | PRIMARY KEY | UUID |
| task_id | VARCHAR(36) | NOT NULL, FK→cron_tasks(id) | 任务 ID |
| triggered_at | DATETIME(3) | NOT NULL | 计划触发时间 |
| started_at | DATETIME(3) | NOT NULL | 实际开始时间 |
| finished_at | DATETIME(3) | | 结束时间 |
| status | VARCHAR(16) | NOT NULL | success / failed / timeout / cancelled |
| output_summary | TEXT | | 输出摘要（可截断） |
| error | TEXT | | 错误信息 |
| delivery_ok | TINYINT(1) | | 投递是否成功 |

```sql
CREATE TABLE cron_run_history (
    id             VARCHAR(36)   NOT NULL PRIMARY KEY,
    task_id        VARCHAR(36)   NOT NULL,
    triggered_at   DATETIME(3)   NOT NULL,
    started_at     DATETIME(3)   NOT NULL,
    finished_at    DATETIME(3),
    status         VARCHAR(16)   NOT NULL,
    output_summary TEXT,
    error          TEXT,
    delivery_ok    TINYINT(1),
    FOREIGN KEY (task_id) REFERENCES cron_tasks(id) ON DELETE CASCADE,
    INDEX idx_cron_run_history_task_id (task_id),
    INDEX idx_cron_run_history_triggered_at (triggered_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.10 ER 关系示意

```
┌─────────────────────────────────────────────────────────────┐
│   agents                                                    │
│   (id, name, workspace, ...)                                │
│   skills 存于 workspace/skills/ 文件系统，由 skills.Index 扫描 │
└─────────────────────────────────────────────────────────────┘
       │
       │ 1   n ┌─────────────────┐ n   1 ┌─────────────┐
       ├───────│  agent_tools    │───────│   tools     │
       │       │ (agent_id,      │       │  (id, ...)  │
       │       │  tool_id)       │       └─────────────┘
       │       └─────────────────┘
       │
       │ 1   n ┌─────────────────┐ n   1 ┌─────────────────┐
       ├───────│  chat_sessions  │───────│  chat_messages  │
       │       │ (id, agent_id,  │       │ (id, session_id,│
       │       │  title)         │       │  role, content)│
       │       └─────────────────┘       └─────────────────┘
       │
       │ 1   n ┌─────────────────┐
       ├───────│  cron_tasks     │─────── delivery_session_id → chat_sessions
       │       │ (id, agent_id,  │
       │       │  schedule_*,    │
       │       │  delivery_*)    │
       │       └────────┬────────┘
       │                │ 1   n ┌──────────────────┐
       │                └───────│ cron_run_history │
       │                        │ (task_id, status)│
       │                        └──────────────────┘
       │
       │       ┌─────────────────┐
       └───────│  channels       │  default_agent → agents
               │ (channel_id,    │
               │  type, webhook) │
               └─────────────────┘
```

### 3.11 常用查询示例

- **按 Agent 查工具**：`SELECT t.* FROM tools t JOIN agent_tools at ON t.id = at.tool_id WHERE at.agent_id = ? ORDER BY at.sort_order`
- **工具是否被绑定**：`SELECT 1 FROM agent_tools WHERE tool_id = ? LIMIT 1`
- **按 Agent 查会话**：`SELECT * FROM chat_sessions WHERE agent_id = ? ORDER BY updated_at DESC`
- **按会话查消息**：`SELECT * FROM chat_messages WHERE session_id = ? ORDER BY created_at ASC`
- **按 channel_id 查渠道**：`SELECT * FROM channels WHERE channel_id = ?`
- **查待执行任务**：`SELECT * FROM cron_tasks WHERE enabled = 1 AND next_run_at <= NOW() ORDER BY next_run_at`
- **按任务查执行历史**：`SELECT * FROM cron_run_history WHERE task_id = ? ORDER BY triggered_at DESC`

---

## 四、存储层接口设计（Store 抽象）

```go
// store/tool_store.go
type ToolStore interface {
    Create(ctx context.Context, tool *ToolCreate) (*ToolMeta, error)
    GetByID(ctx context.Context, id string) (*ToolMeta, error)
    GetByName(ctx context.Context, name string) (*ToolMeta, error)
    List(ctx context.Context, opts ListOptions) ([]ToolMeta, int, error)
    Update(ctx context.Context, id string, updates map[string]any) (*ToolMeta, error)
    Delete(ctx context.Context, id string) error
    ListByAgent(ctx context.Context, agentID string) ([]ToolMeta, error)
}

// store/agent_store.go
type AgentStore interface {
    Create(ctx context.Context, agent *AgentCreate) (*AgentMeta, error)
    GetByID(ctx context.Context, id string) (*AgentMeta, error)
    GetByName(ctx context.Context, name string) (*AgentMeta, error)
    List(ctx context.Context, opts ListOptions) ([]AgentMeta, int, error)
    Update(ctx context.Context, id string, updates map[string]any) (*AgentMeta, error)
    Delete(ctx context.Context, id string) error
    BindTools(ctx context.Context, agentID string, toolIDs []string) error
    UnbindTools(ctx context.Context, agentID string, toolIDs []string) error
}

// store/chat_store.go
type ChatSessionStore interface {
    Create(ctx context.Context, session *ChatSessionCreate) (*ChatSessionMeta, error)
    GetByID(ctx context.Context, id string) (*ChatSessionMeta, error)
    ListByAgent(ctx context.Context, agentID string, opts ListOptions) ([]ChatSessionMeta, int, error)
    Update(ctx context.Context, id string, updates map[string]any) (*ChatSessionMeta, error)
    Delete(ctx context.Context, id string) error
}

type ChatMessageStore interface {
    Create(ctx context.Context, msg *ChatMessageCreate) (*ChatMessageMeta, error)
    GetByID(ctx context.Context, id string) (*ChatMessageMeta, error)
    ListBySession(ctx context.Context, sessionID string) ([]ChatMessageMeta, error)
    DeleteBySession(ctx context.Context, sessionID string) error
}

// store/channel_store.go
type ChannelStore interface {
    Create(ctx context.Context, ch *ChannelCreate) (*ChannelMeta, error)
    GetByID(ctx context.Context, id string) (*ChannelMeta, error)
    GetByChannelID(ctx context.Context, channelID string) (*ChannelMeta, error)
    List(ctx context.Context, opts ListOptions) ([]ChannelMeta, int, error)
    Update(ctx context.Context, id string, updates map[string]any) (*ChannelMeta, error)
    Delete(ctx context.Context, id string) error
}

// store/cron_store.go
type CronTaskStore interface {
    Create(ctx context.Context, task *CronTaskCreate) (*CronTaskMeta, error)
    GetByID(ctx context.Context, id string) (*CronTaskMeta, error)
    List(ctx context.Context, opts CronTaskListOptions) ([]CronTaskMeta, int, error)
    Update(ctx context.Context, id string, updates map[string]any) (*CronTaskMeta, error)
    Delete(ctx context.Context, id string) error
    ListDue(ctx context.Context, before time.Time) ([]CronTaskMeta, error)
    UpdateNextRun(ctx context.Context, id string, nextRunAt time.Time) error
}

type CronRunStore interface {
    Create(ctx context.Context, run *CronRunMeta) error
    ListByTask(ctx context.Context, taskID string, opts ListOptions) ([]CronRunMeta, int, error)
}
```

## 五、REST API 设计

### 5.1 技能管理 API（按 Agent）

技能不单独建表，通过扫描 Agent 的 `{workspace}/skills/` 目录发现与管理技能。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/agents/:id/skills` | 列出该 Agent 下所有技能（扫描 `{workspace}/skills/` 子目录，含 SKILL.md 视为有效） |
| POST | `/api/v1/agents/:id/skills/upload` | 上传技能压缩包（multipart/form-data，file 字段），校验通过后解压到 `{workspace}/skills/` |
| POST | `/api/v1/agents/:id/skills/validate` | 仅校验压缩包（不解压），返回校验结果 |
| DELETE | `/api/v1/agents/:id/skills/:skill_name` | 删除指定技能，移除 `{workspace}/skills/{skill_name}/` 目录及其下所有文件 |

**技能列表响应示例：**

```json
{
  "ret": {"code": 0, "message": "ok"},
  "items": [
    {
      "name": "vm-prelaunch-failure-investigator",
      "description": "分析虚拟机预启动失败原因",
      "path": "/data/workspaces/agent-1/skills/vm-prelaunch-failure-investigator"
    }
  ]
}
```

**技能删除**：`skill_name` 需与目录名一致（kebab-case），删除前校验路径落在 `{workspace}/skills/` 下，防止路径逃逸。

**上传请求示例：**

```
POST /api/v1/agents/:id/skills/upload
Content-Type: multipart/form-data
file: <skill-package.zip>
```

**校验失败响应：**

```json
{
  "valid": false,
  "message": "技能包校验失败",
  "errors": ["缺少 SKILL.md", "scripts/run.sh 路径包含非法字符 ../"]
}
```

### 5.2 工具管理 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/tools` | 创建工具 |
| GET | `/api/v1/tools` | 列表（分页；筛选：`name`, `type`） |
| GET | `/api/v1/tools/:id` | 获取单个工具 |
| PUT | `/api/v1/tools/:id` | 更新工具 |
| DELETE | `/api/v1/tools/:id` | 删除工具（若被绑定返回 409） |

**MCP 工具创建示例：**

```json
// POST /api/v1/tools
{
  "name": "notion-search",
  "description": "搜索 Notion 页面",
  "type": "mcp",
  "config": {
    "mcp_server_id": "notion",
    "mcp_endpoint": "http://localhost:3000/mcp",
    "mcp_backend": "metoro",
    "timeout_sec": 30
  }
}
```

### 5.3 Agent 管理 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/agents` | 创建 Agent（含 workspace） |
| GET | `/api/v1/agents` | 列表（分页） |
| GET | `/api/v1/agents/:id` | 获取 Agent（含绑定工具、workspace 路径、技能列表） |
| PUT | `/api/v1/agents/:id` | 更新 Agent |
| DELETE | `/api/v1/agents/:id` | 删除 Agent（可选级联清理 workspace 下技能） |
| POST | `/api/v1/agents/:id/tools` | 绑定工具 |
| DELETE | `/api/v1/agents/:id/tools` | 解绑工具 |
| GET | `/api/v1/agents/:id/skills` | 列出该 Agent 下技能（见 5.1） |
| DELETE | `/api/v1/agents/:id/skills/:skill_name` | 删除指定技能（见 5.1） |

### 5.4 对话 API（Agent Chat）

#### 5.4.1 会话管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/agents/:id/sessions` | 新建会话（可选 body: `{"title":"会话标题"}`） |
| GET | `/api/v1/agents/:id/sessions` | 获取该 Agent 的会话列表（分页） |
| GET | `/api/v1/sessions/:id` | 获取会话详情 |
| PUT | `/api/v1/sessions/:id` | 更新会话（如标题） |
| DELETE | `/api/v1/sessions/:id` | 删除会话（级联删除消息） |

**新建会话响应示例：**

```json
{
  "id": "uuid",
  "agent_id": "agent-uuid",
  "title": "新对话",
  "created_at": "2025-03-14T10:00:00Z",
  "updated_at": "2025-03-14T10:00:00Z"
}
```

#### 5.4.2 消息发送与历史

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/sessions/:id/messages` | 发送用户消息，触发 Agent 回复 |
| GET | `/api/v1/sessions/:id/messages` | 获取会话内消息历史（按时间升序） |

**发送消息请求：**

```json
// POST /api/v1/sessions/:id/messages
{
  "content": "帮我查一下今天的天气"
}
```

**发送消息响应（非流式）：**

```json
{
  "id": "msg-uuid",
  "session_id": "session-uuid",
  "role": "assistant",
  "content": "根据查询结果，今天北京晴，气温 15-25℃...",
  "created_at": "2025-03-14T10:01:00Z"
}
```

**发送消息响应（流式 SSE）：**

请求头加 `Accept: text/event-stream`，响应为 SSE 流：

```
event: chunk
data: {"content":"根据"}

event: chunk
data: {"content":"查询"}

event: chunk
data: {"content":"结果"}

event: done
data: {"content":"...","done":true}
```

流式模式下，服务端在完成回复后，将 assistant 消息持久化到 `chat_messages`，客户端可通过 `GET /api/v1/sessions/:id/messages` 获取完整历史。

#### 5.4.3 快捷对话（无会话）

为简化前端，支持不创建会话直接对话（适用于单次咨询）：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/agents/:id/chat` | 单轮对话，body 同 `{"content":"..."}`，返回 assistant 回复；不持久化会话与消息 |

#### 5.4.4 技能执行（调试用）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/agents/:id/skills/execute` | 直接执行 Agent 下某技能脚本（路径相对于 workspace/skills） |

### 5.5 消息渠道 API（Channel）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/channels` | 创建渠道 |
| GET | `/api/v1/channels` | 列表（分页；筛选：type、enabled） |
| GET | `/api/v1/channels/:id` | 获取单个渠道 |
| PUT | `/api/v1/channels/:id` | 更新渠道 |
| DELETE | `/api/v1/channels/:id` | 删除渠道 |

**Webhook 入站**（由渠道配置的路径动态注册）：

```
POST /api/v1/webhooks/:channel_id
Content-Type: application/json

{"content": "用户消息", "reply_url": "可选，异步回调 URL"}
```

路由：根据 `channel_id` 查渠道配置 → 校验签名/IP → 取 `default_agent` 或 body 中的 `agent_id` → 调用 Agent 对话 → 同步返回或 POST 到 `reply_url`。

### 5.6 定时任务 API（Cron）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/cron/tasks` | 创建定时任务 |
| GET | `/api/v1/cron/tasks` | 列表（分页；筛选：agent_id、enabled） |
| GET | `/api/v1/cron/tasks/:id` | 获取单个任务 |
| PUT | `/api/v1/cron/tasks/:id` | 更新任务 |
| DELETE | `/api/v1/cron/tasks/:id` | 删除任务 |
| POST | `/api/v1/cron/tasks/:id/run` | 立即执行一次 |
| GET | `/api/v1/cron/tasks/:id/runs` | 获取任务执行历史（分页） |

**创建任务请求示例：**

```json
// POST /api/v1/cron/tasks
{
  "name": "每日摘要",
  "agent_id": "agent-uuid",
  "schedule_kind": "cron",
  "schedule_expr": "0 9 * * *",
  "timezone": "Asia/Shanghai",
  "stagger_sec": 300,
  "payload_kind": "agent_turn",
  "payload_content": "总结昨天的待办与完成情况",
  "timeout_sec": 300,
  "retry_count": 3,
  "retry_interval_sec": 60,
  "delivery": {
    "mode": "webhook",
    "webhook_url": "https://hooks.dingtalk.com/...",
    "secret": "xxx",
    "best_effort": true
  },
  "enabled": true
}
```

### 5.7 对话功能详细设计

本节对应需求文档「三、Agent 对话咨询」，对对话流程、会话管理、消息上下文、流式输出等做详细设计。

#### 5.7.1 对话流程设计

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ 1.选择Agent │ ──► │ 2.进入对话  │ ──► │ 3.输入消息  │ ──► │ 4.Agent回复 │ ──► │ 5.持续对话  │
│ 列表/详情页 │     │ 新建或切换  │     │ 用户发送    │     │ 模型+工具   │     │ 多轮上下文  │
│ 「对话」入口│     │ 会话        │     │ content     │     │ +技能       │     │ 保持        │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

| 步骤 | 前端行为 | 后端行为 | 数据流 |
|------|----------|----------|--------|
| **选择 Agent** | 从 Agent 列表/详情页点击「对话」 | 无 | 跳转至 `/agents/:id/chat` 或 `/agents/:id/sessions` |
| **进入对话** | 新建会话或从会话列表选择已有会话 | `POST /agents/:id/sessions` 或 `GET /sessions/:id` | 返回 session_id，前端进入对话界面 |
| **输入消息** | 用户在输入框输入并发送 | `POST /sessions/:id/messages`，body: `{"content":"..."}` | 持久化 user 消息 |
| **Agent 回复** | 展示流式输出或等待完整响应 | 加载 Agent 配置 → 构建 ReActAgent → 加载历史消息 → 调用 agent.Run → 流式/同步返回 | 持久化 assistant 消息 |
| **持续对话** | 在同一会话内继续输入 | 每次发送均加载完整历史作为上下文 | 多轮对话上下文在 session 内保持 |

#### 5.7.2 会话生命周期

| 状态 | 说明 | 触发 |
|------|------|------|
| **新建** | 用户点击「新对话」或首次进入 Agent 对话 | `POST /agents/:id/sessions` |
| **活跃** | 用户正在该会话内对话 | 发送/接收消息时 `updated_at` 更新 |
| **切换** | 用户切换到另一会话 | `GET /sessions/:id` 加载目标会话 |
| **清空** | 用户清空当前会话消息（可选能力） | `DELETE /sessions/:id/messages` 或前端仅清空展示 |
| **删除** | 用户删除会话 | `DELETE /sessions/:id`，级联删除消息 |

**会话列表排序**：按 `updated_at` 降序，最近活跃的会话排在最前。

#### 5.7.3 消息上下文管理

**上下文窗口策略**：

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| **全量加载** | 加载会话内全部消息作为上下文 | 消息量较小（如 < 50 条） |
| **滑动窗口** | 仅加载最近 N 条消息 | 长对话，控制 token 消耗 |
| **摘要+最近** | 对早期消息做摘要，最近 K 条保留原文 | 超长对话 |

**实现要点**：

- `ListBySession(sessionID)` 按 `created_at` 升序返回消息
- 服务层可配置 `MaxContextMessages`（如 20），超出时仅取最近 N 条
- 消息格式转换为 `agent.Request.Messages`：`{Role: "user"|"assistant", Content: "..."}`

**系统提示词注入**：Agent 的 `system_prompt` 作为首条 system 消息注入上下文，再拼接历史 user/assistant 消息，最后追加当前 user 消息。

#### 5.7.4 流式输出协议（SSE）

**请求**：

```
POST /api/v1/sessions/:id/messages
Content-Type: application/json
Accept: text/event-stream

{"content": "帮我查一下今天的天气"}
```

**响应**（`Content-Type: text/event-stream`）：

| 事件类型 | 说明 | data 格式 |
|----------|------|-----------|
| `chunk` | 增量文本 | `{"content":"根据"}` |
| `chunk` | 继续增量 | `{"content":"查询"}` |
| `done` | 回复结束 | `{"content":"","done":true}` |
| `error` | 发生错误 | `{"error":"模型调用超时"}` |

**客户端处理**：

1. 建立 `EventSource` 或 `fetch` + `ReadableStream`
2. 监听 `chunk` 事件，将 `data.content` 追加到 UI
3. 收到 `done` 后关闭连接，可选调用 `GET /sessions/:id/messages` 拉取最新历史
4. 收到 `error` 时展示错误信息并关闭连接

**服务端实现**：

- 若 sath 支持流式：`agent.RunStream(ctx, req)` 返回 `<-chan string`，逐块写入 SSE
- 若不支持：先同步获取完整回复，再按 chunk 模拟推送（或直接返回非流式 JSON）

#### 5.7.5 工具调用与技能执行

**对话中工具调用**：

- Agent 为 ReActAgent，已绑定工具在会话创建时加载到 `tool.Registry`
- 模型决策调用工具时，由 ReActAgent 执行 `registry.Execute(toolName, params)`
- 工具结果作为 assistant 的 tool_result 消息插入，再继续模型推理
- 前端可展示「正在调用工具 xxx」等中间状态（若 SSE 携带 tool_call 事件）

**对话中技能执行**：

- 技能存于 `{agent.workspace}/skills/`，由 `skills.Index` 扫描
- ReActAgent 可调用 `load_skill`、`execute_skill_script` 等工具
- 脚本执行路径为 `{workspace}/skills/{skill_name}/scripts/xxx.sh`，需校验无路径逃逸

#### 5.7.6 前端界面与交互

| 要素 | 设计要点 |
|------|----------|
| **入口** | Agent 列表每行、Agent 详情页提供「对话」按钮，跳转至该 Agent 的对话页 |
| **Agent 详情** | 展示 Agent 配置、绑定工具、**技能列表**（来自 `GET /agents/:id/skills`）；技能区支持上传、删除 |
| **技能管理** | 技能列表展示 name、description；每行提供「删除」按钮，调用 `DELETE /agents/:id/skills/:skill_name`；删除前二次确认 |
| **对话界面** | 左侧可选会话列表，右侧消息列表 + 底部输入框；支持 Markdown 渲染、代码高亮 |
| **消息列表** | user 消息右对齐或区分样式，assistant 消息左对齐；流式时逐字追加 |
| **输入框** | 支持多行、Enter 发送（可配置 Shift+Enter 换行）；支持停止生成（中断 SSE） |
| **会话列表** | 展示 title、最后更新时间；支持新建、切换、删除；空时展示「暂无会话」 |

**路由建议**：

- `/agents/:id`  Agent 详情（含技能列表与删除）
- `/agents/:id/chat` 或 `/agents/:id/chat/:sessionId` 对话页（无 sessionId 时自动新建会话）

#### 5.7.7 异常与重试

| 错误类型 | 处理策略 |
|----------|----------|
| 会话不存在 | 返回 404，前端提示并跳转至新建会话 |
| Agent 不存在 | 返回 404，前端提示并返回 Agent 列表 |
| 模型调用超时 | 返回 504 或 SSE error 事件，前端提示可重试 |
| 工具调用失败 | 将错误信息作为 tool_result 返回模型，由模型决定是否重试或向用户说明 |
| 技能执行失败 | 同上，错误信息反馈给模型 |

**重试**：客户端可对 `POST /sessions/:id/messages` 做有限重试（如 1 次），需保证幂等或避免重复持久化 user 消息。

#### 5.7.8 权限与隔离

- 会话与 Agent 强绑定，`session.agent_id` 必须与路径中的 `agent_id` 一致
- 校验逻辑：`GET /sessions/:id` 时，若 `session.agent_id != path.agent_id`，返回 403
- 未来扩展多租户时，可在 session/agent 表增加 `user_id` 或 `tenant_id`，查询时按租户过滤

---

## 六、sath 扩展实现路径

### 6.1 对话（Chat）实现

详见 **5.5 对话功能详细设计**。实现要点：

1. **数据层**：新增 `store/chat_store.go`，实现 `ChatSessionStore`、`ChatMessageStore`，对应 `chat_sessions`、`chat_messages` 表。
2. **服务层**：`service/chat_service.go`：
   - `CreateSession(agentID, title)`：创建会话
   - `SendMessage(sessionID, content)`：保存 user 消息 → 按 `session.agent_id` 加载 Agent → 构建 `ReActAgent`（含 tools、skills）→ 加载会话历史作为上下文（见 5.5.3）→ 调用 `agent.Run` 或流式接口 → 保存 assistant 消息
   - `ListMessages(sessionID)`：返回会话内消息列表
3. **Agent 构建**：复用 6.4 的 Agent 构建逻辑，按 `AgentMeta` 创建 `model.Model`、`tool.Registry`（注册绑定工具）、`skills.Index`（扫描 workspace/skills），组装 `ReActAgent`。
4. **流式输出**：按 5.5.4 的 SSE 协议实现；若 sath 支持流式，则 `SendMessage` 返回 `io.Reader` 或 channel，handler 以 SSE 形式推送；否则先同步获取完整回复再返回。
5. **Handler**：`api/chat/handler.go` 暴露 REST 接口，按 5.5.8 校验 `session.agent_id` 与路径一致，防止越权。

### 6.2 技能管理（发现、上传、删除）

#### 6.2.1 技能发现与列表

1. **数据来源**：技能存于 `{agent.workspace}/skills/` 文件系统，每个子目录为一个技能。
2. **发现逻辑**：遍历 `{workspace}/skills/` 下子目录，若子目录内存在 `SKILL.md` 则视为有效技能。
3. **实现**：复用 `skills.Index` 或直接 `os.ReadDir` + 解析 SKILL.md 获取 `name`、`description`。
4. **API**：`GET /api/v1/agents/:id/skills` → 按 `agent_id` 取 `AgentMeta.Workspace` → 扫描 `{workspace}/skills/` → 返回 `[]SkillMeta`。

**流程**：

```
GET /agents/:id/skills
    → AgentUsecase.Get(id) 获取 workspace
    → 若 workspace/skills 不存在，返回 []
    → skills.Index.All() 或 遍历子目录 + 读取 SKILL.md
    → 返回 [{name, description, path}, ...]
```

#### 6.2.2 技能包上传与校验

1. **上传接口**：`POST /api/v1/agents/:id/skills/upload` 接收 multipart 压缩包，先写入临时目录。
2. **校验逻辑**：`validator/skill_package.go` 实现 `ValidateSkillPackage(zipPath string) (*ValidateResult, error)`：
   - 压缩包格式合法、可解压
   - 包含 SKILL.md
   - 脚本扩展名符合 .sh/.py/.js
   - 无路径逃逸（`../`、绝对路径等）
3. **解压**：校验通过后，解压到 `{agent.workspace}/skills/`。
4. **与 Index 集成**：Agent 运行时，`skills.Index` 扫描 `{workspace}/skills/` 目录，供 `load_skill`、`execute_skill_script` 使用。

#### 6.2.3 技能删除

1. **API**：`DELETE /api/v1/agents/:id/skills/:skill_name`。
2. **校验**：
   - `skill_name` 仅允许字母、数字、连字符（kebab-case），禁止 `..`、`/` 等。
   - 目标路径必须为 `{workspace}/skills/{skill_name}`，经 `filepath.Clean` 后校验落在 `{workspace}/skills/` 下。
3. **执行**：`os.RemoveAll(targetPath)` 删除目录及内容。
4. **错误**：技能不存在返回 404；路径校验失败返回 400。

**流程**：

```
DELETE /agents/:id/skills/:skill_name
    → AgentUsecase.Get(id) 获取 workspace
    → 校验 skill_name 格式（无 .. / 等）
    → targetPath = filepath.Join(workspace, "skills", skill_name)
    → 校验 filepath.Clean(targetPath) 以 workspace/skills/ 为前缀
    → os.Stat(targetPath)，不存在则 404
    → os.RemoveAll(targetPath)
    → 返回 200
```

### 6.3 工具管理扩展

1. **持久化**：新增 `store/tool_store.go`，存储工具元数据。
2. **类型区分**：`ToolMeta.Type` 为 `builtin` 或 `mcp`，创建 MCP 工具时调用现有 `RegisterMcpTool(reg, &McpConfig{...})`。
3. **按 Agent 加载**：Agent 运行时，根据 `ToolIDs` 从 `ToolStore` 取配置，动态注册到 `tool.Registry`，再交给 `ReActAgent` 等使用。

### 6.4 Agent 管理扩展

1. **持久化**：新增 `store/agent_store.go`，存储 Agent 配置（含 `workspace`）及 `tool_ids` 绑定关系。
2. **运行时构建**：根据 `AgentMeta` 构建 `agent.Agent`：
   - 从 `model.Factory` 按 `ModelConfig` 创建 `model.Model`
   - `skills.Index` 扫描 `{workspace}/skills/` 加载技能
   - 从 `ToolStore` 按 `ToolIDs` 加载工具，注册到 `tool.Registry`
   - 调用 `agent.NewReActAgent(model, registry, ...)` 或 `NewChatAgent`，注入 `SystemPrompt`。

### 6.6 消息渠道（Channel）实现

#### 6.6.1 架构与路由

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Web 请求   │     │  API 请求    │     │ Webhook 入站 │
│  (channel=  │     │ (channel=   │     │ (channel=   │
│   web)      │     │  api)       │     │  webhook)   │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           ▼
              ┌─────────────────────────┐
              │   Channel 路由中间件     │
              │ - 识别渠道类型            │
              │ - 校验认证/签名/IP        │
              │ - 解析 agent_id          │
              └────────────┬────────────┘
                           ▼
              ┌─────────────────────────┐
              │   Agent 对话服务         │
              │ - 构建 ReActAgent        │
              │ - 执行对话/技能          │
              └────────────┬────────────┘
                           ▼
              ┌─────────────────────────┐
              │   回复投递               │
              │ - Web: 会话/SSE 返回     │
              │ - API: 响应体返回        │
              │ - Webhook: 同步/异步回调 │
              └─────────────────────────┘
```

#### 6.6.2 实现要点

1. **Web 渠道**：现有 Web 对话即为 `channel=web`，会话创建时选定 Agent，无需额外 Channel 表记录；可选在 `chat_sessions` 增加 `channel` 字段便于统计。
2. **API 渠道**：现有 `POST /agents/:id/chat` 即为 API 渠道，请求路径中的 `agent_id` 即路由；可选增加 API Key 认证中间件，按 Key 绑定默认 Agent。
3. **Webhook 渠道**：
   - 配置存储于 `channels` 表，`type=webhook` 时需 `webhook_path`、`webhook_secret`、`ip_whitelist`。
   - 动态注册路由：`POST /api/v1/webhooks/{channel_id}` 或使用统一入口 `POST /api/v1/webhooks` + body 中 `channel_id`。
   - 签名校验：钉钉用 `timestamp`+`secret` 的 HMAC-SHA256；飞书、Slack 等各有算法，可抽象 `WebhookVerifier` 接口，按 `channel_id` 选择实现。
   - 请求体解析：`{"content":"...", "agent_id":"可选", "reply_url":"可选"}`；`reply_url` 非空时异步回调，否则同步返回。
4. **SessionKey 格式**：`agent:{agent_id}:channel:{channel_type}:peer:{peer_id}`，Web 的 peer 可为 session_id，API 可为 request_id，Webhook 可为来源系统 ID。

#### 6.6.3 Webhook 入站流程

```
POST /api/v1/webhooks/dingtalk
    → ChannelStore.GetByChannelID("dingtalk")
    → 校验 IP 白名单
    → 校验签名（timestamp + secret）
    → 解析 body: content, agent_id?, reply_url?
    → agent_id = body.agent_id ?? channel.default_agent
    → ChatService.SendMessage(agentID, content) 或等价调用
    → 若 reply_url 非空：异步 POST 结果到 reply_url
    → 否则：同步返回 JSON 响应
```

### 6.7 定时任务（Cron）实现

#### 6.7.1 调度器架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Cron Scheduler (后台 goroutine)            │
│  - 每 10~60s 扫描 cron_tasks WHERE enabled=1 AND next_run_at<=NOW() │
│  - 按 next_run_at 排序，逐个执行                             │
│  - 错峰：next_run_at + random(0, stagger_sec)               │
└────────────────────────────┬────────────────────────────────┘
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                    Task Executor                             │
│  - agent_turn: 构建 ReActAgent，发送 payload_content，取回复  │
│  - skill_execute: 调用 ExecuteSkillScript                    │
│  - 超时控制：context.WithTimeout                             │
│  - 失败重试：按 retry_count、retry_interval_sec               │
└────────────────────────────┬────────────────────────────────┘
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                    Delivery (结果投递)                        │
│  - none: 仅写 cron_run_history                               │
│  - webhook: POST JSON 到 delivery_webhook_url，可选 HMAC 签名 │
│  - session: ChatMessageStore.Create(role=assistant, 摘要)    │
└─────────────────────────────────────────────────────────────┘
```

#### 6.7.2 实现要点

1. **next_run_at 计算**：
   - `cron`：使用 `github.com/robfig/cron/v3` 或 `github.com/gorhill/cronexpr` 解析表达式，计算下次触发时间。
   - `every`：`last_run + schedule_expr` 秒。
   - `at`：单次任务，`schedule_expr` 为 ISO 时间戳；执行后 `enabled=false` 或删除。
2. **错峰**：`actual_run_at = next_run_at + rand.Intn(stagger_sec)`，避免整点雪崩。
3. **执行流程**：
   - `CronRunStore.Create` 插入 run 记录，status=running。
   - 执行 agent_turn 或 skill_execute。
   - 更新 run：finished_at、status、output_summary、error。
   - 若配置了 delivery，执行投递，更新 delivery_ok。
   - `CronTaskStore.UpdateNextRun` 更新任务的 next_run_at。
4. **投递实现**：
   - **webhook**：`POST delivery_webhook_url`，body 为 `{"task_id","task_name","status","output_summary","error","triggered_at","finished_at"}`；若 `delivery_secret` 非空，添加 `X-Signature: HMAC-SHA256(body, secret)`。
   - **session**：校验 `delivery_session_id` 所属 session 的 agent_id 与任务 agent_id 一致；`ChatMessageStore.Create` 插入 role=assistant 的消息，content 为 Markdown 格式的摘要。
5. **立即执行**：`POST /api/v1/cron/tasks/:id/run` 直接调用 Executor，不更新 next_run_at。

#### 6.7.3 目录结构扩展

```
portal/
├── api/
│   ├── tool/
│   ├── agent/
│   ├── chat/
│   ├── channel/      # Channel CRUD、Webhook 入站 handler
│   └── cron/         # Cron 任务 CRUD、立即执行、执行历史
├── store/
│   ├── ...
│   ├── channel_store.go
│   ├── cron_task_store.go
│   └── cron_run_store.go
├── service/
│   ├── ...
│   ├── channel_service.go   # Webhook 入站处理、签名校验
│   └── cron_service.go       # 任务 CRUD、调度循环、执行、投递
├── scheduler/                # 可选独立包
│   └── cron_scheduler.go    # 后台扫描、触发执行
└── ...
```

### 6.8 目录结构建议（完整）

```
sathweb/  (或 portal)
├── api/
│   ├── tool/       # 工具 CRUD handler
│   ├── agent/      # Agent CRUD、技能管理（列表、上传、删除）
│   ├── chat/       # 会话管理、消息发送、流式 Chat handler
│   ├── channel/    # Channel CRUD、Webhook 入站
│   └── cron/       # 定时任务 CRUD、立即执行、执行历史
├── store/
│   ├── tool_store.go
│   ├── agent_store.go
│   ├── chat_store.go
│   ├── channel_store.go
│   ├── cron_task_store.go
│   └── cron_run_store.go
├── service/
│   ├── tool_service.go
│   ├── agent_service.go
│   ├── chat_service.go
│   ├── channel_service.go
│   └── cron_service.go
├── scheduler/
│   └── cron_scheduler.go    # 定时任务调度循环
├── validator/
│   └── skill_package.go
└── cmd/serve/
```

## 七、与 sath 的集成方式

| 集成点 | 说明 |
|--------|------|
| **skills.Index** | Agent 的 `workspace/skills` 目录作为 Index 扫描路径，`NewIndex([]string{agent.Workspace+"/skills"}, ...)` |
| **tool.Registry** | 每个 Agent 会话创建独立 `Registry`，按 `ToolIDs` 注册内置工具 + MCP 工具 |
| **tool.RegisterMcpTool** | MCP 工具创建时保存配置，运行时按配置调用 |
| **tool.RegisterExecuteSkillScriptTool** | 技能执行沿用现有实现，脚本路径为 `{workspace}/skills/` 下相对路径 |
| **agent.ReActAgent** | 按 `AgentMeta` 构建时传入 `Registry`、`SystemPrompt`、技能 Index；Chat 时加载会话历史作为上下文 |
| **agent.ChatAgent** | 若仅需纯对话（无工具调用），可用 `ChatAgent` 替代 `ReActAgent` |
| **流式输出** | 若 sath 支持流式，Chat 接口以 SSE 推送增量内容；否则同步返回完整回复 |
| **events.Bus** | 工具/技能执行已发布 `ToolInvoked`、`ToolExecuted`，可对接 obs 做统计 |
| **Channel** | 无 sath 直接依赖；Web/API 复用现有 Chat 服务；Webhook 为新增 HTTP 入口，校验后调用 Agent |
| **Cron** | 无 sath 直接依赖；调度器为独立 goroutine；执行时复用 Agent 构建与 `execute_skill_script` |
