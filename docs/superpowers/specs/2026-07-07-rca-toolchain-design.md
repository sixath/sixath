# 线上问题排查(RCA)工具链 — 设计文档

- 日期:2026-07-07
- 状态:待评审
- 场景:线上问题根因分析。从 Jaeger trace 链路 + ELK 日志定位现象,再回到多仓库代码找根因。

## 1. 目标与非目标

### 目标
在 `sixath/framework` 中补齐**五个 RCA 专属原生工具**,注册进 `tool.Registry`,让 Agent 能自主完成
"trace → 日志 → 代码" 的 RCA 闭环。所有代码侧工具统一采用**多仓库根语义**(与单仓库的
`search_files` / `read_file` 区分开,避免混用):

1. `jaeger_trace` — 查询 Jaeger 链路
2. `es_log_query` — 查询 ELK 业务日志
3. `rca_grep` — 跨多仓库按内容正则搜(对标 Grep)
4. `rca_glob` — 跨多仓库按文件名/路径找(对标 Glob)
5. `rca_read` — 读某仓库某文件,带行号(对标 Read)

### 非目标(本期不做)
- 自动 clone / pull 仓库(本期只在**已存在的本地目录**中检索;自动同步留作后续迭代)
- Jaeger 鉴权(线上 Jaeger **无鉴权**,裸 HTTP 访问)
- 新增 tracing 数据源抽象(Jaeger 用独立轻量 HTTP 客户端,不进 datasource 体系)
- 把工具串成固定排查 Skill(先跑通工具,编排交给 Agent;固化 Skill 是后续话题)

## 2. 形态与依据

五个工具均为框架原生工具,遵循现有 `RegisterXxxTool(reg *Registry) error` 模式
(对齐 `tool/http_tool.go`、`tool/ssh_exec.go`、`tool/cronjob_tool.go`),
返回结构化 `map[string]any`,由 Registry 统一包裹 trace span 与事件。

**复用现有底层(实现复用,非工具复用):**
- `es_log_query` 复用 `executor.ESExecutor`(`executor/elasticsearch.go`):已有只读拦截、超时、
  MaxRows、可观测;ES 连接复用**已注册的 ES datasource**(`datasource/elasticsearch.go`)。
- `rca_grep` / `rca_glob` 复用 `file_tools.go` 里的 `searchWithRipgrep` / `searchFilesByGlob`
  底层函数,对多个仓库根循环调用。
- `rca_read` 复用 `read_file` 的读取逻辑,root 限定在 `rca.repos.roots`。
- 三个代码工具均用 `tool/pathguard.go` 做路径白名单守卫,防止越权读到白名单外的目录。
- `jaeger_trace` 因框架无 tracing 数据源,新写一个最简 HTTP 客户端(仅 GET,无鉴权)。

## 2.1 为何做 RCA 专属工具而非复用通用工具

RCA 的"搜内容 / 找文件 / 读文件"三类能力,框架已有单仓库版原生工具
(`search_files` / `read_file`),但它们通过 `workspaceRootFromCtx` 绑定**单一 workspace root**,
用 `ResolveWorkspacePath` 把检索限制在该 root 内,**结构上只能作用于一个仓库**。

RCA 场景要跨多个仓库根(service-a / service-b …)检索,且需要**统一的多仓库语义**——
命中结果标明来自哪个仓库、可选限定某仓库、越权守卫覆盖所有 root。因此代码侧做成
`rca_` 前缀的专属工具组,与单仓库通用工具语义隔离、不混用(经确认的设计取向)。

**"跑命令/看 git"(Bash)不做专属版**:继续复用现有 `terminal` / `ssh_exec`,本期不新增
`rca_git`(经确认)。

## 3. 工具划分与边界

| 工具 | 文件 | 职责 | 底层 |
|------|------|------|------|
| `jaeger_trace` | `framework/tool/jaeger_tool.go` | 按 traceID 拉整条链路 / 按条件搜 trace,返回结构化 span | 新写 HTTP 客户端(Jaeger Query API,无鉴权) |
| `es_log_query` | `framework/tool/es_log_tool.go` | 按 `trace_id` 或时间窗+关键字查 ELK 日志 | 复用 `executor.ESExecutor`(只读) |
| `rca_grep` | `framework/tool/rca_code_tools.go` | 跨多仓库根按内容正则搜代码,返回带行号片段 | 复用 `searchWithRipgrep`,对多 root 循环,`pathguard` 守卫 |
| `rca_glob` | `framework/tool/rca_code_tools.go` | 跨多仓库根按文件名/路径 glob 找文件 | 复用 `searchFilesByGlob`,对多 root 循环,`pathguard` 守卫 |
| `rca_read` | `framework/tool/rca_code_tools.go` | 读某仓库某文件,返回带行号内容 | 复用 `read_file` 读取逻辑,root 限定在白名单 |

> 三个代码工具同放 `rca_code_tools.go`,共享多仓库根解析与 `pathguard` 守卫逻辑。

**统一的多仓库语义(三个代码工具共享):**
- 检索范围 = `rca.repos.roots` 白名单里的所有仓库根。
- 入参统一支持可选 `repo`(string):不填=作用于全部仓库;填了=限定该仓库根。
- 出参统一带 `repo` 字段,标明命中来自哪个仓库。
- 越权由 `pathguard` 守卫:所有路径必须落在某个 root 内,否则拒绝。

**边界原则:** 各工具单一职责、互不依赖,独立可测。以 `trace_id` 作为串联的公共键;
由 Agent(或后续排查 Skill)按 `trace_id` 编排,工具本身不互调。

## 4. 配置与鉴权

密钥仅走环境变量,禁止硬编码(框架规范)。config.yaml 放非敏感部分:

```yaml
rca:
  jaeger:
    query_url: http://jaeger-host:16686      # Jaeger Query API 基址;无鉴权
  es:
    datasource_id: "es-logs"                  # 指向【已注册】的 ES datasource(鉴权走该 datasource 的 User/Password,来自 env)
    default_index: "app-logs-*"               # 默认业务日志索引
    trace_id_field: "trace_id"                # 日志中关联 trace 的字段名
  repos:
    roots:                                    # rca_grep/rca_glob/rca_read 的多仓库根白名单(pathguard)
      - D:/workspace/repos/service-a
      - D:/workspace/repos/service-b
```

- Jaeger:无任何鉴权 env。
- ES:不新增连接配置,复用已注册 datasource;凭证沿用该 datasource 现有 env(如 `*_USER` / `*_PASSWORD`)。
- repos:`roots` 为空时三个代码工具返回明确错误(未配置检索目录),不静默返回空。

## 5. 各工具输入/输出契约

### 5.1 `jaeger_trace`
- 入参:
  - `trace_id` (string,优先):精确拉取整条链路
  - 或搜索模式:`service` (string)、`operation` (string)、`start`/`end` (RFC3339 或相对时间)、`tags` (object)、`limit` (number, 默认 20)
- 出参:
  - `spans[]`:`service`、`operation`、`start`、`duration_ms`、`status`/`error`、`tags`(关键子集)
  - `errors[]`:带 error 标记的 span 摘要(service、operation、错误信息)
  - `services[]`:链路涉及的服务名去重列表(供 `rca_grep` 反查仓库)
- 行为:`trace_id` 与搜索参数二选一;都缺时返回参数错误。

### 5.2 `es_log_query`
- 入参:
  - `trace_id` (string,优先):按 `trace_id_field` 精确匹配
  - 或:`query` (string,关键字)、`index` (string,默认 `default_index`)、`start`/`end`、`level` (string)、`limit` (number, 默认 50)
- 出参:`hits[]`(`timestamp`、`level`、`service`、`message`、异常栈字段若有)、`total`
- 行为:构造 Search DSL,经 `ESExecutor` 只读通道执行,自动享有超时/MaxRows/写拦截。

### 5.3 `rca_grep`
- 入参:`pattern` (string,正则,必填)、`repo` (string,可选,限定某仓库根)、`glob` (string,可选,文件名过滤)、`max_results` (number, 默认 100)
- 出参:`matches[]`(`repo`、`file`、`line`、`snippet`)、`truncated` (bool)
- 行为:在 `rca.repos.roots` 全部根内检索(或 `repo` 指定的单根);每条路径经 `pathguard` 校验;超过 `max_results` 置 `truncated=true`。

### 5.4 `rca_glob`
- 入参:`pattern` (string,glob,必填)、`repo` (string,可选)、`max_results` (number, 默认 100)
- 出参:`matches[]`(`repo`、`file`)、`truncated` (bool)
- 行为:多仓库根内按 glob 匹配文件路径,守卫与 `truncated` 同 `rca_grep`。

### 5.5 `rca_read`
- 入参:`repo` (string,必填,指定仓库根)、`file` (string,必填,仓库内相对路径)、`start_line`/`end_line` (number,可选)
- 出参:`repo`、`file`、`content`(带行号)、`total_lines`
- 行为:路径经 `pathguard` 校验必须落在指定 `repo` 根内;越界拒绝。

## 6. 测试与验收

### 单元测试(每个工具配 `_test.go`)
- `jaeger_tool_test.go`:用 `httptest.Server` 打桩 Jaeger Query API,验证 traceID 拉取与搜索两条路径、error span 提取、services 去重。
- `es_log_tool_test.go`:沿用 executor 现有 ES 打桩方式,验证 trace_id 精确查询与关键字查询、只读通道生效。
- `rca_code_tools_test.go`:临时目录造多仓库结构,验证 `rca_grep`/`rca_glob` 跨多根命中与 `repo` 字段、`rca_read` 读取、白名单外路径被拒、`truncated` 行为、roots 为空报错。

### 收尾
```bash
cd framework
go test ./tool/... -v
go test ./...
```

### 验收标准(端到端,人工)
给定一个真实 traceID:
1. `jaeger_trace` 返回出错 span 与涉及服务名
2. `es_log_query` 用同一 `trace_id` 拉到对应日志与异常栈
3. `rca_grep` 用异常类名 / 方法名跨多仓库定位到具体代码行,`rca_read` 读出上下文

## 7. 后续迭代(本期不实现,记录方向)
- 代码工具增加按 git URL 自动 clone/pull 到缓存目录的能力
- 把五个工具编排成固定排查 Skill(load_skill 触发一键 RCA)
- Jaeger 若未来需要鉴权,补 Bearer/Basic env 支持
- 视需要新增 `rca_git`(限定仓库根的只读 git log/blame)
