# Fork Chat（从消息分叉会话）设计

**日期**: 2026-09-16  
**状态**: 已确认（2026-09-16）  
**方案**: 一次 Fork RPC，服务端快照权威数据  
**关联**:
- 会话管理 / `parent_session_id`：[framework 会话管理设计](../../../framework/docs/superpowers/specs/2026-05-25-session-management-design.md)
- Rewind：`portal/internal/service/rewind.go`、`POST /api/v1/sessions/{session_id}/rewind`
- L2 compact 子会话（无关，保持默认关）：`portal/internal/chat/fork_on_compact.go`

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 产品语义 | 从某条消息分叉：新会话带走该点及之前的历史，**原会话完整保留、仍可写** |
| 入口 | 仅 Web `ChatPage`；消息旁与「Rewind here」并排「Fork from here」 |
| 成功后 | 立刻切换到新 `session_id` |
| API | `POST /api/v1/sessions/{session_id}/fork`，body `{ "message_id" }`；原始 HTTP，**不改** `chat.proto` |
| 拷贝范围 | 消息（含 metadata）+ 对应 turn traces + session scope `memory_units`（仅 active）+ 进程内 todo |
| 实现 | Portal `ChatService.ForkToMessage` 编排；MySQL 权威数据同一事务 |
| 对偶 | Rewind 改当前会话；Fork 开平行线。二者都按 `message_id` 锚定 |

---

## 1. 目标与非目标

### 目标

1. 用户可在任意 active 的 user/assistant 消息上 Fork，得到独立可写的子会话。
2. 子会话 `parent_session_id` 指向原会话；侧栏已有「分支」标签无需改结构。
3. 子会话在锚点处的对话、工具轨迹、session 记忆、todo 与分叉瞬间一致（见 §4 精度）。
4. 原会话一条消息都不 inactive、不 readonly。

### 非目标（一期不做）

- Gateway、`/runtime/v1`、企微等渠道入口
- 侧栏按 `parent_session_id` 树形折叠
- 虚拟共用父会话行（copy-on-write overlay）
- 浏览器 / 终端 / process 会话状态
- `ask_user` / `skill_manage` 未决确认
- 企微 peer 绑定
- 幂等 Fork（连点两次允许两个子会话）
- 改 L2 `ForkSessionOnCompactIfEnabled` 行为
- 把 todo 持久化到 MySQL

---

## 2. 现状（实现必须对齐）

| 已有 | 缺口 |
|------|------|
| `chat_sessions.parent_session_id`；`CreateSession` 可写 parent | 不拷贝消息 |
| 侧栏 `parent_session_id` →「分支」badge | 无用户 Fork 按钮 |
| Rewind：同会话 soft-hide 锚点及之后 | 会丢掉后续，不能开平行线 |
| Compact fork：父只读 + 摘要种子，默认关 | 不是用户分叉 |

Web 已有 `chatApi.rewindSession` → `POST /sessions/{id}/rewind`（Portal `RewindHandler`）。Fork 用同一风格，不走 proto RPC。

---

## 3. 架构

```
ChatPage  「Fork from here」
    │  POST /api/v1/sessions/{id}/fork  { message_id }
    ▼
ForkHandler  →  ChatService.ForkToMessage
    │
    ├─ 校验会话 ACL + 锚点消息
    ├─ MySQL 事务
    │     CreateSession(parent_session_id=原)
    │     拷贝消息前缀（新 ID）
    │     拷贝 traces（新 session_id / request_id）
    │     拷贝 session memory（新 scope_id）
    ├─ 事务提交后（失败只打日志）
    │     TodoStore 按 session_id 复制
    │     session search 索引新消息
    ▼
SessionReply + 拷贝计数  →  前端切换 sessionId
```

**边界**

- 编排只在 Portal。Framework ReAct 不感知 Fork。
- 不接 Gateway。Web 继续打 Portal `/api/v1`。
- Compact fork 与本能力独立：本能力从不 `MarkSessionReadonly`。

---

## 4. API

`POST /api/v1/sessions/{session_id}/fork`

**请求**

```json
{ "message_id": "<锚点 chat_messages.id>" }
```

**成功 200**

```json
{
  "session_id": "<子会话 id>",
  "parent_session_id": "<原会话 id>",
  "title": "Fork of …",
  "copied_messages": 0,
  "copied_traces": 0,
  "copied_memory_units": 0,
  "copied_todos": 0
}
```

计数字段以实际写入为准；todo 拷贝失败时 `copied_todos` 为 0 且 HTTP 仍 200。

**错误**

| 条件 | 码 |
|------|-----|
| `session_id` / `message_id` 空 | 400 `INVALID_ARGUMENT` |
| 会话不存在或非当前用户 | 与 Rewind 相同的 404 |
| 消息不存在或不属于该会话 | 404 `FORK_MESSAGE_NOT_FOUND` |
| 消息 `active=false` | 400 `FORK_INACTIVE` |

父会话 `readonly=true` **允许** Fork（只读快照，子会话可写）。

流式进行中：前端禁用按钮；服务端不返回 409，按事务开始时已提交的 active 行快照。

---

## 5. 拷贝语义

### 5.1 消息（权威）

1. 取出该会话全部 `active=true` 的消息，按 `(created_at, id)` 升序。
2. 找到锚点下标；**取闭区间前缀**（含锚点）。锚点不在 active 列表则走 `FORK_INACTIVE` / `FORK_MESSAGE_NOT_FOUND`。
3. 每条插入子会话：新 UUID；**保留原 `created_at`**；`role`/`content`/`active=true` 原样；`metadata` 深拷贝后写入 `forked_from_message_id`。
4. 不复用原主键。timeline metadata 内的 tool 事件原样复制，不尝试改写内部 id。

与 Rewind 对偶：Rewind 从锚点起藏掉后缀；Fork 把含锚点的前缀抄走。前缀以排序下标为准，避免同秒多条消息时 SQL `created_at` 比较漏拷兄弟行。

### 5.2 Turn traces（权威）

拷贝原会话中 `active` 且 `created_at <= 锚点.created_at` 的 traces（与 Rewind `DeactivateAfter` 的 `created_at >= at` 对偶）。

写入子会话时：新行主键、`session_id=子`、新 `request_id`（避免 `(session_id, request_id)` 唯一键冲突）。可在 trace metadata 记 `forked_from_request_id`（有则写，无则不加字段）。

`TurnTraceStore` 若现有 `ListBySession` 带 limit、不够全量，则新增 store 方法（例如 `CopyActiveUntil` 或无截断 list），**不要**在 Fork 路径静默截断 traces。

### 5.3 Session memory（权威）

对 `Scope=session`、`ScopeID=原 session_id`、`Status=active` 的 units：`List` 后 `Remember` 到 `ScopeID=子 session_id`。

- 只拷当前 active 快照，**不**复制 supersede 链。
- 新 unit id；内容与 metadata 复制。
- user / agent scope 不拷。

### 5.4 Todo（非权威）

`tool.DefaultTodoStore`：`List(父)` → `Replace(子, items)`。父列表不变。Portal 多实例时 todo 本就不跨进程，Fork 与现网 todo 同命运，不为此做分布式存储。

### 5.5 子会话元数据

- `agent_id` / `user_id` 与父相同
- `parent_session_id` = 父 id
- `title` = `Fork of ` + 父 title（空则 `Fork`）；截断到列长 256
- `readonly=false`，`rewind_count=0`

---

## 6. 组件

| 层 | 职责 |
|----|------|
| `web/src/pages/ChatPage.tsx` | 「Fork from here」与 Rewind 并排；`streaming` / 进行中禁用；成功后切换 `sessionId` 并刷新侧栏 |
| `web/src/api/client.ts` | `forkSession(sessionId, messageId)` |
| `portal/internal/server/fork.go` | `ForkHandler`，路由旁挂 Rewind |
| `portal/internal/service/fork.go` | `ForkToMessage` 编排 |
| `portal/internal/biz` + `data/chat_mysql.go` | 前缀查询 + 批量插入消息 |
| `portal/internal/data/turn_trace_mysql.go` | 全量拷贝 until 锚点 |
| `SessionUnitsBackend`（已有 List/Remember） | 记忆快照；不必新接口，除非 List 分页需要在 Fork 里循环 |
| `framework/tool/todo_tool.go` | `Copy(fromSession, toSession)` 或等价 List+Replace |

不新增 proto；不改 Gateway。

---

## 7. 事务与失败

1. **校验阶段不写库。**
2. **消息 + traces + memory_units + 新 session 行** 同一 MySQL 事务。任一步失败则回滚：不得留下无消息的子会话。
3. **提交后**：todo、FTS 索引尽最大努力。失败只打日志，HTTP 仍 200，计数字段反映实际成功量。
4. 不引入分布式事务。

FTS：对拷贝出的每条 user/assistant 消息走现有 `NotifySessionMessageIndexed`（或等价批量）；失败不影响 Fork 成功。

---

## 8. 前端行为

- 按钮出现条件与 Rewind 相同：`m.id && sessionId && !streaming && (role === user \| assistant)`。
- 另用 `forking` 状态，避免双提交；Rewind 进行中也可禁用 Fork（反之亦然）。
- 成功：把路由/状态中的 session 换成返回的 `session_id`，触发 `SessionSidebar` 刷新（依赖现有 `sessionId` 变化已会 `loadSessions`）。
- 失败：页面内错误提示，留在原会话。
- 文案：`Fork from here`（与现有英文 Rewind 按钮一致）。title：`Start a new session with history up to this message; the original chat is unchanged`。

---

## 9. 测试

**后端**

- 锚点前缀出现在子会话；父会话后缀仍 active。
- inactive 消息不出现在子会话。
- 锚点之后的 traces 不拷；子 traces 的 `session_id` 为子会话。
- session memory：`scope_id` 为子会话；父 units 仍在。
- todo：父不变，子为副本；两 session_id 隔离。
- 模拟 traces 写入失败：事务回滚，无新 `chat_sessions` 行。
- 错误码：错会话消息、inactive 锚点。
- readonly 父会话仍可 Fork。

**前端**

- 按钮在 Rewind 旁；streaming 时两者禁用。
- `forkSession` 成功后当前会话变为新 id。

一期不强制 live e2e；有现成 rewind e2e 时可仿一条，非阻塞。

---

## 10. 实现顺序（供计划，非本期编码）

1. Store/repo：消息前缀读取、trace 全量 until、todo `Copy`
2. `ForkToMessage` + HTTP handler + 单测
3. Web 按钮与 `forkSession`
4. 回滚与 ACL 测试

---

## 11. 验收

- 在一条中间 assistant 消息上 Fork：新会话停在该句；原会话仍能看到该句之后的内容并继续发消息。
- 侧栏新会话带「分支」；点回原会话历史完整。
- 子会话内工具轨迹 / todo / session 记忆与锚点前状态一致（todo 仅当 Fork 落在同一 Portal 进程时保证，与现网 todo 语义相同）。
