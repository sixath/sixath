# RCA 进实例：`vm_run_cmd`（`:53000/runCmd`）

> 状态：已评审  
> 日期：2026-09-19  
> 关联：`framework/tool/jaeger_tool.go`、`framework/tool/ssh_exec.go`、`framework/tool/terminal_tool.go`、`portal/internal/chat/rca_builder.go`、`docs/superpowers/specs/2026-09-19-tool-egress-proxy-design.md`  
> 触发：根因分析需要看虚拟机本机日志/进程，这些日志不进 ES；现网 Agent 用 `http_request` 打 `:53000` 会截断 URL、把空 200 当成「没日志」。

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 形态 | RCA 原生工具 `vm_run_cmd`，不是 Skill 包一层 `http_request`，也不并进 `ssh_exec` |
| 协议 | `POST http://<host>:<port>/runCmd`，`Content-Type: application/json`，body `{"cmd":"<原样字符串>"}`，无鉴权头 |
| 响应 | HTTP **200** 为成功（现场合同）；其它 2xx 也当成功以免中间层改写状态。body 当 `text/plain`（可空）。非 2xx 为失败。不解析 PowerShell 表格 |
| 寻址 | `host` 与 `vmid` 至少一个；有 host 直连；只有 vmid 则查 MySQL |
| 查 IP | 固定 SQL：`SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?` |
| 命令 | 开放 `cmd`；硬拒绝灾难命令；其余危险命令走现有 `confirm_token` |
| 出网 | 复用工具行 egress / Agent 默认代理（`netx` HTTP Client） |

一句话：进实例只走本工具；查到 IP 再 POST；空输出不是没日志；危险操作要用户点确认。

## 1. 背景

现有 RCA 闭环是 Jaeger → ES → `rca_grep`/`rca_glob`/`rca_read`。云游戏等实例上的进程日志、落盘目录**不采集到 ES**。每台实例监听 **53000**，`runCmd` 在机器上执行命令（现场样例为 PowerShell）。

现网失败模式：

- `http_request` 把 URL 写成 `http://10.x.x.x:53000/runC` 一类截断。
- HTTP 200 空 body 被模型写成「目录不存在 / 没有日志」。
- 开放命令没有 `ssh_exec` 那套拒绝/确认。
- 只有 `vmid` 时还要先 `execute_read` 查 IP，步骤容易丢。

`ssh_exec` 是 SSH，协议和寻址都不同，一期不复用成「远程执行」抽象。

## 2. 目标与非目标

### 目标（一期）

1. 绑定 RCA 工具后，Agent 可对实例执行任意 `cmd`（受 §5 策略约束）。
2. 入参 `host` 和/或 `vmid`；无 host 时用已绑定 MySQL 查 `mgr_ipv4_address`。
3. 危险命令走与 `terminal` / `execute_write` 相同的 `confirm_token` 用户确认。
4. 成功/失败结构化返回（`ok`、`error_code`、`evidence_refs`）；空 stdout 显式标记，不得伪装成业务空结果。
5. 出站走已有命名代理解析；连接失败带 `transient`，文案可含代理 id，不含密码。
6. `rca-investigation` Skill 增加「进实例」步骤：禁止为此改用 `http_request`。

### 非目标（一期不做）

- 改实例侧 `:53000` 服务、加鉴权、换路径。
- 固定动作 API（`tail_log` / `list_processes`）；开放 cmd 已覆盖。
- 解析 PowerShell `Format-Table` 为 JSON。
- 禁止或劫持 `http_request`（只在说明和 Skill 里引导）。
- Portal 可配 SQL 模板；查 IP 的表名/SQL 写死。
- 自动 kill / 重启 / 变更配置（用户确认后仍可手工下这些 cmd）。
- 把本工具做成 SSH 后端，或让模型传完整 URL。

### 诚实上限

- `runCmd` 无鉴权：能打到 `:53000` 就能执行。门禁是 Agent 绑定 + 命令策略 + 确认，不是实例侧身份。
- 查 IP 依赖表 `t_game_virtual_machine_info` 与字段 `mgr_ipv4_address` 仍存在；表迁了要改代码。
- 确认列表以 Windows 实例为主；Linux 风格命令也会命中部分规则，但不保证穷尽。
- 测连通不等于业务日志一定在某路径。

## 3. 架构

```text
Agent 绑定
  RCA 工具 func_path=vm_run_cmd
  MySQL 数据源（查 IP，可选指定 datasource_id）
  egress_mode / Agent.proxy_id

BuildRegistry
  先注册 MySQL datasource
  registerRCATool(vm_run_cmd)：
    HTTPClient = netx 解析结果
    Lookup = 绑定的 MySQL（见 §4.2）

vm_run_cmd.Execute
  校验 host/vmid/cmd
  命令策略 → 拒绝 | pending 确认 | 继续
  无 host → SQL 查 IP
  POST /runCmd
  截断 text/plain → 结构化 map
```

工具实现放 `framework/tool/`（建议 `vm_run_cmd.go`，单文件 + 测试）。Portal 在 `rca_builder` 增加 `func_path` 分支；**查 IP 的 Lookup 在 Execute 时解析**，不要求 `RegisterVMRunCmd` 发生在 MySQL `Register` 之前。

现网 `BuildRegistry`（`portal/internal/chat/agent_builder.go`）先扫 RCA、后 `registerDatasourceTools` 才 `datasource.NewRegistry()`。一期**必须**改 `agent_builder.go`：循环里只记下 `vm_run_cmd` 配置，等 `registerDatasourceTools` 返回后再 `RegisterVMRunCmd`，并把同一个 `dsReg`/executor 以窄接口注入：`LookupVMIP(ctx, vmid) (host string, ambiguous bool, err error)`。

不得在工具内部自己 `sql.Open`。无 MySQL 时工具仍注册：只传 `host` 可用；只传 `vmid` 才 `permanent`。

YAML / `sath serve`：现有结构是 `rca.jaeger` / `rca.es` 小节，没有 Portal 的 `func_path`。一期加可选 `rca.vm_run_cmd`（例如 `enabled: true`）；**未配置则不注册**，不要给所有 CLI 进程默认开进实例。查库用 YAML 里已有的 MySQL 数据源；没有 MySQL 时允许 `host` 直连。

## 4. 调用契约

### 4.1 入参（模型可见）

| 字段 | 约束 |
|------|------|
| `cmd` | 必填。原样写入 JSON `cmd`，不做 shell 拆词 |
| `host` | 可选。仅主机名或 IPv4，禁止 `://`、路径、userinfo、空格 |
| `vmid` | 可选。整数（十进制字符串可 parse）；禁止拼进 SQL |
| `port` | 可选。默认 **53000**，范围 1–65535 |
| `timeout_sec` | 可选。默认 30，上限 120 |
| `confirm_token` | 危险命令二次调用时必填 |

`host` 与 `vmid` **至少一个**。两者都有时以 `host` 为准，结果里仍带回 `vmid` 标注。

最终 URL **只由运行时拼接**：`http://<host>:<port>/runCmd`。模型传 `url` 字段 → `permanent`，不发请求。

### 4.2 查 IP

无 `host`、有 `vmid`：

```sql
SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?
```

- `?` 绑定已解析的整型 vmid。
- 使用 Agent 已绑定、类型为 MySQL 的数据源。
- RCA 配置可选 `datasource_id`（Portal 工具名 / datasource id）：指定则只用那一个。
- 未指定且 MySQL 数据源 **恰好一个** → 用它。
- 未指定且 **0 个** → `permanent`：未绑定 MySQL，请传 `host` 或绑定数据源。
- 未指定且 **>1 个** → `permanent`：列出 id/name，要求配置 `datasource_id`。
- 0 行或地址为空/空白 → `permanent`，**不** POST `:53000`。
- 多行：取第一条非空地址，返回 `ip_ambiguous: true`。
- 查询失败（库不可达）→ `transient`。

查库走现有只读执行通路（与 `execute_read` 同源：超时、MaxRows=小、只 SELECT）。**禁止**把整行机器表或密码回给模型；只暴露解析出的 `host`、`vmid`、`ip_ambiguous`。

### 4.3 请求与响应

请求：

```http
POST /runCmd HTTP/1.1
Host: <host>:<port>
Content-Type: application/json

{"cmd":"<cmd 原样>"}
```

成功：HTTP **200**（现场）；**201–299 也当成功**。body 当 UTF-8 文本（现场 `Content-Type: text/plain`）。空 body 合法。

失败：非 2xx；`ok: false`，附 `http_status` 与截断正文。

截断：对齐 `ssh_exec` 默认约 50KiB；超长 `truncated: true`。

超时：默认 30s，上限 120s；超时 `error_code=transient`。

### 4.4 成功/失败形状

成功：

```json
{
  "ok": true,
  "host": "10.141.12.13",
  "vmid": 199306,
  "port": 53000,
  "stdout": "",
  "output_empty": true,
  "truncated": false,
  "evidence_refs": [{"kind": "vm_run_cmd", "host": "10.141.12.13", "vmid": "199306"}]
}
```

- `output_empty` 仅当 `ok` 且截断前 stdout 长度为 0。
- **不要**复用 ES 的 `hit_status=empty`。
- 模型断言「目录不存在 / 没有日志」必须引用 `stdout` 中的原句。

失败：`ok: false`，`error`，`error_code`=`transient`|`permanent`。

危险命令待确认（**钉死，禁止 `error=confirm_required` 充数**）：工具返回必须与 `terminal` 同形，前端才会出卡片：

```json
{
  "status": "pending",
  "token": "<opaque>",
  "command": "<cmd 原文>",
  "expires_in": 300,
  "hint": "user must confirm; re-call vm_run_cmd with confirm_token to execute"
}
```

`portal/internal/service/chat_stream.go` 的 `confirmationRequestsFromResponse` 增加抽取：`ToolName=="vm_run_cmd"` 且 `status=="pending"` 且 `token`、`command` 非空 → `ChatConfirmationRequest{Kind:"vm_run_cmd", Title:"Confirm instance command", Description:"Review the instance command before it is executed.", DSL: command, Token, ExpiresIn, Severity:"danger"}`。`Description` 必填：Web `parseConfirmRequiredPayload` 缺 description 会静默不出卡片。用户确认后走与 `terminal` 相同路径：下轮带 `confirm_token` 再调工具（**不必**做 `skill_manage` 那种跳过 LLM 的 `Apply*Confirm`）。非法/过期 token 复用 terminal 的 `ConfirmTokenError`（`not_found` / TTL），不另开合同。

仓库里没有共享 `ConfirmStore` 类型。`vm_run_cmd` **自建** pending store（接口对齐 `TerminalPendingStore`：Save/Get/Delete by sessionID+token），**不要**复用 `TerminalPendingStore` 实例以免 token 串台。Portal 装配时 `NewInMemory…` 注入，与 terminal 一样从 `ContextKeySessionID` 取会话。未注入 store 时危险命令返回 `confirm_required_but_unconfigured`，不静默执行。

## 5. 命令策略

分类对 **整段 `cmd` 字符串** 做大小写不敏感子串/正则匹配（与 `terminal`/`ssh_exec` 相同思路）。Windows 为主。

### 5.1 硬拒绝（不可确认，不发 HTTP）

至少包括：`format `、`format.com`、`Remove-Item -Recurse` 打到盘符根、`rm -rf /`、`del /s /q C:`、`shutdown /s`、`Stop-Computer`、`Reset-ComputerMachinePassword`。

返回 `permanent` + `blocked_by_policy`。

### 5.2 需 `confirm_token`

至少包括：`taskkill`、`Stop-Process`、`Stop-Service`、`net stop`、`sc stop`、`sc delete`、`Restart-Computer`、`shutdown /r`、`Remove-Item`、`del `、`rmdir`、`rd `。

无 token：不发 HTTP，返回 §4.4 的 `status=pending` 对象。  
有合法 token：从本工具 pending store 取出原 `cmd`（以 store 为准，防模型改命令），POST 后删除 token（一次性）。

### 5.3 直接执行

未命中 5.1/5.2 的命令，包括只读：`Get-ChildItem`、`Get-Process`、`Get-Content`、`type `、`tasklist`、`powershell -Command` 包着的只读脚本。若 PowerShell 包装里仍含 5.1/5.2 关键字，按内层命中（对整串匹配即可覆盖）。

### 5.4 确认存储

见 §4.4：自建 pending store，session 级一次性 token，TTL 默认 300s（对齐 terminal）。

## 6. 错误处理

| 情况 | 合同 |
|------|------|
| 缺 cmd / 缺 host 且缺 vmid / 非法 host / 非法 vmid | `permanent`，不发 HTTP |
| 硬拒绝 | `permanent`，`blocked_by_policy` |
| 查 IP 0 行、地址空、多 MySQL 未指定 | `permanent` |
| MySQL 超时/网络 | `transient` |
| 代理 TCP/握手失败、runCmd 超时 | `transient`；若走了代理，文案含代理 **id/name**，无密码 |
| HTTP 4xx/5xx | `ok: false`；4xx 偏 `permanent`，5xx/超时偏 `transient`（实现时钉死一张表，测单测） |
| 未知 proxy_id | 装配或执行 fail-closed，与出网代理 spec 一致 |

日志：`vmid`、`host`、`port`、代理 id、HTTP 状态；禁止完整 cmd 若超长可截断；禁止密码。

## 7. Portal / Skill

- `portal/api/tool/v1/tool.proto`：`RCAConfig.func_path` 增加 `vm_run_cmd`；复用已有 `datasource_id` 表示查 IP 的 MySQL。
- `biz` 允许的 RCA func_path 列表加上 `vm_run_cmd`。
- `registerRCATool` 增加分支；未知 path 仍 skip。
- ToolForm：RCA 子类型下拉增加「实例 runCmd」；可选填查 IP 用的 MySQL `datasource_id`（与 Agent 上将绑定的 MySQL 工具名对齐）。多 MySQL 未填则运行时 `permanent`，不在保存时猜。
- `framework/skills_examples/skills/rca-investigation/SKILL.md`：标准顺序增加第 4 步（实例未采集日志 / 本机文件与进程）。`allowed_tools` 加上 `vm_run_cmd`。写明：仅有 vmid 不是进机理由；禁止 `http_request` 打 `:53000`；空 `stdout` 不是没日志。
- 全局 skills prompt 若仍写「进实例」，改为点名 `vm_run_cmd`（若该文件在本期会误导模型则改；不顺带改压缩/重述文案）。

出网：`toolEgressBinding(cfg)` + `RegistryBuildOptions.Proxies`，HTTP Client 注入方式对齐 `jaeger_trace`。

## 8. 测试合同

假 HTTP（`httptest`）+ 假 MySQL（内存/fake executor），不连真机。

1. 带 `host`：观察到 POST `/runCmd`、JSON `{"cmd":...}`、200 正文进入 `stdout`。
2. 只有 `vmid`：先查询再 POST；0 行时 HTTP 次数为 0。
3. 入参含 `url` 或 `host` 带 `://` → `permanent`。
4. `taskkill` 无 token → 返回 `status=pending` + `token` + `command`，HTTP 次数 0；带合法 `confirm_token` → POST。`chat_stream` 单测：`ToolName=vm_run_cmd` 的 pending 结果能抽出确认卡片。
5. `format` → `blocked_by_policy`，HTTP 次数 0。
6. HTTP 200 空 body → `ok` 且 `output_empty`，error 为空。
7. Client 超时 / 代理不可用 → `transient`。
8. 两个 MySQL 且未配 `datasource_id` → `permanent`。
9. `func_path=vm_run_cmd` 装配后 registry 有该工具；egress SOCKS5 时请求经自定义 Dialer（对标 Jaeger 测试手法）。

不测：真实 `:53000`、表格解析、Linux 穷尽命令表。

## 9. 文件边界（给计划用）

| 单位 | 职责 |
|------|------|
| `framework/tool/vm_run_cmd.go` | 策略、寻址、POST、截断、结果 map |
| `framework/tool/vm_run_cmd_test.go` | §8 工具级单测 |
| `portal/internal/chat/agent_builder.go` | 在 MySQL 注册之后（或共享 dsReg）再挂 `vm_run_cmd` |
| `portal/internal/chat/rca_builder.go` | `func_path` 分支 + 注入 Client 与 Lookup |
| `portal/internal/chat/rca_builder_test.go` | 能注册；无 MySQL 仍注册；只 host 可跑 |
| `portal/internal/service/chat_stream.go` | pending → 确认卡片（`Kind=vm_run_cmd`） |
| `portal/internal/service/chat_stream_test.go` | 抽取确认请求 |
| proto + ToolForm + Skill | 产品面；ToolForm 可选 `datasource_id` |

Lookup 不要做成第二个「模型可调 SQL 工具」；它是 `vm_run_cmd` 的内部依赖。
