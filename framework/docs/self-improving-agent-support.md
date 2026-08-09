# 支持 self-improving-agent 所需能力分析

本文档分析 framework 若要支持 [ClawHub self-improving-agent](https://clawhub.ai/pskoett/self-improving-agent) 技能，当前还需补齐的功能能力。

---

## 一、self-improving-agent 核心能力概览

| 能力 | 说明 |
|------|------|
| **学习日志** | 向 `.learnings/` 下的 LEARNINGS.md、ERRORS.md、FEATURE_REQUESTS.md 追加内容 |
| **Hooks** | UserPromptSubmit（任务后提醒）、PostToolUse（工具执行后错误检测） |
| **Promotion** | 将学习内容提升到 CLAUDE.md、AGENTS.md、SOUL.md、TOOLS.md |
| **跨会话** | sessions_list、sessions_history、sessions_send、sessions_spawn |
| **Workspace 注入** | 每次会话自动注入 AGENTS.md、SOUL.md、TOOLS.md 等 |
| **Skill 提取** | 从学习记录中提取为新 Skill |

---

## 二、Framework 已有能力

| 能力 | 状态 | 说明 |
|------|------|------|
| load_skill / read_skill_file | ✅ | 加载 Skill 正文和捆绑文件 |
| execute_skill_script | ✅ | 执行 scripts/ 下脚本（.sh/.py/.js/.ps1） |
| 事件总线 | ✅ | RunStarted、ToolInvoked、ToolExecuted、RunCompleted、RunError 等 |
| MCP | ✅ | 可挂载外部 MCP 服务 |
| 插件系统 | ✅ | 可注册工具、中间件、事件监听器 |

---

## 三、需要补齐的能力

### 1. 工作区文件读写工具（高优先级）

**缺口**：没有 `write_file` / `append_file` 类工具，无法写入 `.learnings/` 等文件。

**建议**：

- 新增 `write_file` / `append_file` 工具，或
- 通过 MCP filesystem 服务提供（如 `@modelcontextprotocol/server-filesystem`），并在配置中显式挂载。

**约束**：路径需限制在工作区根目录内，避免越权访问。

---

### 2. 工作区根路径（Workspace Root）

**缺口**：框架没有「工作区根路径」概念，Skill 无法知道项目目录。

**建议**：

- 在 `agent.Request` 或 `context` 中增加 `WorkspaceRoot string`
- 或在配置中增加 `workspace_root`，由 Portal/应用层传入
- 供 `write_file`、`append_file` 等工具解析相对路径

---

### 3. Hook 机制（中优先级）

**缺口**：没有 UserPromptSubmit、PostToolUse 等 Hook 点。

**现状**：已有 `ToolExecuted` 等事件，可近似实现 PostToolUse。

**建议**：

| Hook | 对应事件/扩展点 | 实现方式 |
|------|-----------------|----------|
| **PostToolUse** | `ToolExecuted` | 订阅 `ToolExecuted`，在 payload 中拿到 tool、input、output、error，调用 error-detector 脚本 |
| **UserPromptSubmit** | 无 | 需新增：在用户消息加入对话前发布 `UserMessageSubmitted` 或类似事件；或在 `RunStarted` 时注入「提醒评估学习」的提示 |

**实现要点**：

- 在 `agent.Request` 处理用户消息时增加事件发布
- 或通过中间件在请求前注入「是否记录学习」的提示

---

### 4. 工具输出传递给 Hook 脚本

**缺口**：error-detector 需要读取 `CLAUDE_TOOL_OUTPUT` 等环境变量。

**建议**：

- 在 `ToolExecuted` 的订阅逻辑中，执行 error-detector 时设置环境变量，例如：
  - `CLAUDE_TOOL_OUTPUT` = 工具 output（或 error）
  - `TOOL_NAME`、`TOOL_INPUT` 等
- 或通过 `execute_skill_script` 的 `input` 参数传入 JSON，由脚本解析

---

### 5. 会话级 Workspace 文件注入（中优先级）

**缺口**：无法在每次会话开始时自动注入 AGENTS.md、SOUL.md、TOOLS.md 等。

**建议**：

- 在 System Prompt 构建时，若配置了 `workspace_bootstrap_files`，则读取这些文件并拼接到 system prompt
- 或提供 `read_workspace_file(path)` 工具，由模型按需读取

---

### 6. 跨会话工具（低优先级，可选）

**缺口**：没有 sessions_list、sessions_history、sessions_send、sessions_spawn。

**说明**：这些是 OpenClaw 的会话管理能力，与 framework 的 `memory.Memory` 不同。

**建议**：

- 若 Portal 有会话存储，可新增 MCP 服务或专用工具暴露这些能力
- 或作为后续扩展，不阻塞 self-improving-agent 的基础支持

---

### 7. Skill 元数据扩展（低优先级）

**缺口**：Skill frontmatter 不支持 `always`、`hooks` 等配置。

**建议**：

- 在 `SkillMeta` 中增加可选字段，例如：
  - `Always bool`：是否在每次会话自动加载
  - `Hooks []HookConfig`：如 `{type: "PostToolUse", matcher: "Bash", script: "error-detector.sh"}`

---

## 四、实现优先级建议

| 优先级 | 能力 | 工作量 | 说明 |
|--------|------|--------|------|
| P0 | 工作区文件读写 | 中 | 通过 MCP filesystem 或内置 write_file/append_file |
| P0 | 工作区根路径 | 小 | 在 Request/Config 中增加 workspace_root |
| P1 | PostToolUse 等价能力 | 小 | 基于 ToolExecuted 订阅 + 脚本调用 |
| P1 | 工具输出传给脚本 | 小 | 环境变量或 input 参数 |
| P2 | UserPromptSubmit 等价 | 小 | 新事件或 RunStarted 时注入提示 |
| P2 | Workspace 文件注入 | 中 | System Prompt 构建时读取并注入 |
| P3 | 跨会话工具 | 大 | 依赖 Portal 会话存储，可后置 |

---

## 五、最小可行方案（MVP）

要支持 self-improving-agent 的核心流程（记录学习、错误、修正），至少需要：

1. **工作区文件写入**：`append_file(path, content)` 或 MCP filesystem 的 write 能力，路径限制在 workspace_root 内。
2. **工作区根路径**：在请求上下文中传入 `workspace_root`。
3. **PostToolUse 等价**：订阅 `ToolExecuted`，在 output 含错误时调用 error-detector 脚本（通过 `execute_skill_script` 或直接 exec），并传入工具输出。

在此基础上，可逐步增加 UserPromptSubmit、Workspace 注入和跨会话能力。

---

## 六、架构设计

### 6.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Portal / 应用层                                    │
│  - 传入 workspace_root、session_id、user_id                                   │
│  - 构建 Request、注入 Metadata                                                │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Framework 层                                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐ │
│  │   Agent      │  │   Events     │  │   Skills     │  │   Workspace      │ │
│  │ (ReActAgent) │  │   Bus        │  │   Index      │  │   (新增)          │ │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘ │
│         │                 │                 │                     │           │
│         │                 │                 │                     │           │
│  ┌──────▼──────────────────▼─────────────────▼─────────────────────▼────────┐ │
│  │                        tool.Registry                                     │ │
│  │  load_skill | read_skill_file | execute_skill_script | write_file | ...  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                      │                                        │
│  ┌──────────────────────────────────▼──────────────────────────────────────┐ │
│  │                     context 传递                                          │ │
│  │  request_id | workspace_root | session_id | (hook_env for scripts)        │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 上下文传递链

| 层级 | 职责 | 传递内容 |
|------|------|----------|
| Portal | 请求入口 | 设置 `Request.Metadata["workspace_root"]`、`Request.RequestID` |
| Handler | 构建 context | `WithValue(ctx, ContextKeyWorkspaceRoot, root)` |
| Registry.Execute | 工具执行 | 从 ctx 读取 `workspace_root`，解析相对路径 |
| Hook 订阅者 | 执行脚本 | 从 Event.Payload 取 tool/output，设置 env 后 exec |

### 6.3 新增/扩展组件

| 组件 | 位置 | 职责 |
|------|------|------|
| `tool/workspace.go` | 新增 | `write_file`、`append_file`、`read_workspace_file` 工具 |
| `tool/tool.go` | 扩展 | `ContextKeyWorkspaceRoot` 等 context 键定义 |
| `events/event.go` | 扩展 | 新增 `UserMessageSubmitted` 事件 Kind |
| `skills/meta.go` | 扩展 | `SkillMeta` 增加 `Always`、`Hooks` 字段 |
| `skills/index.go` | 扩展 | 解析 frontmatter 中的 `hooks` 配置 |
| `config/config.go` | 扩展 | `WorkspaceConfig`、`SkillsConfig.WorkspaceBootstrapFiles` |
| `agent/agent.go` | 扩展 | `Request` 增加 `WorkspaceRoot`，Run 前发布 `UserMessageSubmitted` |
| Hook 执行器 | 新增 | 订阅 `ToolExecuted`，按 Skill hooks 配置调用脚本 |

### 6.4 数据流：PostToolUse Hook

```
Tool 执行完成
    │
    ▼
Registry 发布 ToolExecuted (tool, input, output, error)
    │
    ▼
Hook 执行器订阅收到事件
    │
    ├─ 遍历已加载 Skill 的 hooks 配置
    ├─ 若 matcher 命中（如 tool=="execute_skill_script" 或 tool 为 Bash 类）
    │
    ▼
设置环境变量: TOOL_NAME, TOOL_INPUT, CLAUDE_TOOL_OUTPUT, TOOL_ERROR
    │
    ▼
exec.Command(scriptPath).Env(env).Run()
    │
    ▼
脚本可读取 env，决定是否触发学习记录
```

### 6.5 数据流：Workspace 文件注入

```
Handler 构建 System Prompt
    │
    ├─ 读取 cfg.Skills.WorkspaceBootstrapFiles (如 ["AGENTS.md", "SOUL.md"])
    ├─ 若 workspace_root 非空，按相对路径读取
    │
    ▼
拼接: systemPrompt + "\n\n---\n\n" + fileContents
    │
    ▼
注入到 llmReq.Messages[0] (system role)
```

---

## 七、实现细节设计

### 7.1 工作区根路径（Workspace Root）

**扩展点**：`agent.Request`、`context`、`config`

**agent.Request 扩展**：

```go
// agent/agent.go
type Request struct {
    Messages     []model.Message
    Metadata     map[string]any
    RequestID    string
    WorkspaceRoot string  // 新增：工作区根目录；为空时 workspace 工具不可用
}
```

**context 键**：

```go
// tool/tool.go 扩展（与 ContextKeyRequestID 同文件）
const (
    ContextKeyRequestID     = "request_id"      // 已有
    ContextKeyWorkspaceRoot = "workspace_root"   // 新增
)
```

**Handler 侧注入**（以 Portal 为例）：

```go
// portal/internal/service/chat.go
req := &agent.Request{
    Messages:      messages,
    WorkspaceRoot: agentMeta.WorkspaceRoot,  // 从 Agent 配置或 workspace 获取
    RequestID:     requestID,
}
ctx = context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, req.WorkspaceRoot)
```

**获取优先级**：`Request.WorkspaceRoot` > `Request.Metadata["workspace_root"]` > `config.Workspace.Root`

---

### 7.2 工作区文件读写工具

**工具定义**（`tool/workspace.go` 新建）：

```go
// write_file: 覆盖写入文件
// 参数: path (相对 workspace_root), content (string)
// 约束: path 经 filepath.Clean 后不得含 ".."，需在 workspace_root 内

// append_file: 追加写入文件
// 参数: path, content
// 约束: 同上；若文件不存在则创建

// read_workspace_file: 读取工作区文件（按需加载，替代或补充 bootstrap）
// 参数: path (相对 workspace_root)
// 约束: 同上
```

**注册**：

```go
func RegisterWorkspaceTools(reg *Registry, getWorkspaceRoot func(context.Context) string) error
```

`getWorkspaceRoot` 从 context 读取：`ctx.Value(tool.ContextKeyWorkspaceRoot).(string)`。若为空，工具执行时直接返回 `"workspace_root not set"` 错误。

**路径校验**：

```go
func resolveWorkspacePath(root, rel string) (string, error) {
    cleaned := filepath.Clean(rel)
    if strings.Contains(cleaned, "..") {
        return "", errors.New("path must not escape workspace")
    }
    abs := filepath.Join(root, cleaned)
    if !strings.HasPrefix(filepath.Clean(abs), filepath.Clean(root)) {
        return "", errors.New("path must be under workspace")
    }
    return abs, nil
}
```

**配置**：

```go
// config/config.go
type WorkspaceConfig struct {
    Root string `json:"workspace_root" yaml:"workspace_root"`
}

type Config struct {
    // ...
    Workspace WorkspaceConfig `json:"workspace" yaml:"workspace"`
}
```

---

### 7.3 Hook 机制：事件与 Hook 执行器

**新增事件**：

```go
// events/event.go
const (
    // ...
    UserMessageSubmitted Kind = "agent.user_message.submitted" // 用户消息即将加入对话
)
```

**发布时机**：在 ReActAgent 将 `req.Messages` 合并到 `messages` 之后、首次调用模型之前，发布一次 `UserMessageSubmitted`，Payload 含 `last_user_message`（最后一条 user 消息内容）。

**Hook 配置结构**（在 Skill frontmatter 中）：

```yaml
# SKILL.md frontmatter 扩展
hooks:
  - type: PostToolUse
    matcher: Bash          # 或 tool 名称，如 execute_skill_script
    script: scripts/error-detector.sh
  - type: UserPromptSubmit
    script: scripts/activator.sh
```

**SkillMeta 扩展**：

```go
// skills/meta.go
type HookConfig struct {
    Type   string `yaml:"type"`   // PostToolUse | UserPromptSubmit
    Matcher string `yaml:"matcher"` // 工具名或 "Bash" 等
    Script string `yaml:"script"`  // 相对 Skill 根目录的脚本路径
}

type SkillMeta struct {
    // ... 现有字段
    Always bool         `yaml:"always"`  // 是否每次会话自动加载
    Hooks  []HookConfig `yaml:"hooks"`  // Hook 配置
}
```

**Hook 执行器**（新建 `plugin/hooks.go` 或 `agent/hooks.go`）：

```go
type HookExecutor struct {
    skillsIdx *skills.Index
    loadedSkills map[string]skills.SkillMeta  // 当前请求已 load_skill 的 Skill
}

func (h *HookExecutor) OnToolExecuted(ctx context.Context, e events.Event) {
    payload := e.Payload
    toolName, _ := payload["tool"].(string)
    output := payload["output"]
    errVal := payload["error"]

    for _, meta := range h.loadedSkills {
        for _, hook := range meta.Hooks {
            if hook.Type != "PostToolUse" { continue }
            if !matchTool(hook.Matcher, toolName) { continue }
            h.runHookScript(ctx, meta, hook.Script, map[string]string{
                "TOOL_NAME":         toolName,
                "CLAUDE_TOOL_OUTPUT": fmt.Sprint(output),
                "TOOL_ERROR":         fmt.Sprint(errVal),
            })
        }
    }
}
```

**loadedSkills 维护**：在 `load_skill` 的 Execute 中，将加载的 Skill 写入 context 或 request 级别的 `loadedSkills` 集合。Hook 执行器需能访问该集合，可考虑通过 context 传递：`ctx.Value(ContextKeyLoadedSkills)`。

---

### 7.4 工具输出传递给 Hook 脚本

**环境变量约定**：

| 变量名 | 含义 |
|--------|------|
| `TOOL_NAME` | 工具名称 |
| `TOOL_INPUT` | 工具输入（JSON 序列化） |
| `CLAUDE_TOOL_OUTPUT` | 工具输出或错误信息 |
| `TOOL_ERROR` | 非空表示工具执行失败 |

**执行时设置**：

```go
cmd := exec.CommandContext(ctx, "sh", scriptPath)
cmd.Env = append(os.Environ(),
    "TOOL_NAME="+toolName,
    "CLAUDE_TOOL_OUTPUT="+outputStr,
    "TOOL_ERROR="+errStr,
)
cmd.Dir = skillRoot
cmd.Run()
```

---

### 7.5 会话级 Workspace 文件注入

**配置**：

```go
// config/config.go - SkillsConfig 扩展
type SkillsConfig struct {
    // ...
    WorkspaceBootstrapFiles []string `json:"workspace_bootstrap_files" yaml:"workspace_bootstrap_files"`
}
// 示例: ["AGENTS.md", "SOUL.md", "TOOLS.md"]
```

**注入逻辑**（在 `templates.BuildSkillsAwarePrompt` 或 Handler 中）：

```go
func buildSystemPromptWithWorkspaceBootstrap(sys string, workspaceRoot string, files []string) string {
    if workspaceRoot == "" || len(files) == 0 {
        return sys
    }
    var b strings.Builder
    b.WriteString(sys)
    for _, rel := range files {
        full := filepath.Join(workspaceRoot, rel)
        data, err := os.ReadFile(full)
        if err != nil {
            continue
        }
        b.WriteString("\n\n---\n\n## ")
        b.WriteString(rel)
        b.WriteString("\n\n")
        b.Write(data)
    }
    return b.String()
}
```

---

### 7.6 跨会话工具（后续扩展）

**接口设计**（由 Portal 实现，通过 MCP 或直接注册工具暴露）：

```go
// sessions_list: 返回 session_id 列表
// sessions_history: session_id -> 历史消息
// sessions_send: 向 session_id 发送 learning
// sessions_spawn: 创建子 Agent 会话
```

**依赖**：Portal 的 session 存储、会话元数据。Framework 不直接实现，仅定义工具契约（名称、参数、返回格式），由 Portal 实现 Execute。

---

### 7.7 实现顺序与依赖

| 阶段 | 任务 | 依赖 |
|------|------|------|
| 1 | ContextKeyWorkspaceRoot、Request.WorkspaceRoot | 无 |
| 2 | RegisterWorkspaceTools (write_file, append_file, read_workspace_file) | 阶段 1 |
| 3 | UserMessageSubmitted 事件、发布时机 | 无 |
| 4 | SkillMeta.Hooks、frontmatter 解析 | 无 |
| 5 | HookExecutor、订阅 ToolExecuted | 阶段 4 |
| 6 | loadedSkills 维护（load_skill 时登记） | 阶段 5 |
| 7 | WorkspaceBootstrapFiles 配置与注入 | 阶段 1 |
