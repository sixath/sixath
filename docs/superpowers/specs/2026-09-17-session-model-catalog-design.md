# 会话级模型选择与平台模型目录

**日期**: 2026-09-17  
**状态**: 已确认（2026-09-17）  
**方案**: 平台供应商 + 目录缓存；会话覆盖 `provider_id` + 模型名；每轮发消息动态组客户端  
**关联**:
- Agent 模型配置：`web/src/pages/AgentForm.tsx`、`framework/model/factory.go` `NewModelFromConfig`
- 发消息：`portal/internal/service/chat.go` `SendMessage` / `SendMessageStream`
- Runtime 桥：`portal/internal/runtime` `/runtime/v1/turns`（不改请求体，读会话覆盖）
- Fork 会话：`docs/superpowers/specs/2026-09-16-fork-chat-design.md`（子会话拷贝覆盖列）

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 谁选模型 | **聊天时**选，不改 Agent 表单一期 |
| 目录来源 | **平台**供应商（中转站 + 直连 OpenAI / DashScope），所有能进该 Agent 会话的人共用 |
| 接入类型一期 | `openai_compat`（官方 OpenAI 或 OpenAI 兼容中转站）+ `dashscope`；**不做** Ollama |
| 覆盖范围 | **当前会话**；新会话回到 Agent `model_config` |
| 选择器入口 | 仅 Web ChatPage |
| 渠道 | Gateway / 企微 **无选择器**，发到同一 `session_id` 时 **沿用** 已保存覆盖 |
| 目录维护 | 同步 `/v1/models` + 隐藏 / 改展示名 / 手工加；同步不覆盖已改的展示名与隐藏 |
| 可见性 | 启用供应商下未隐藏条目 + 「Agent 默认」；无每 Agent 白名单 |
| 绑定方式 | 每轮请求读会话列 → `NewModelFromConfig`；不改 Agent 行；进行中的流不热切换 |
| 会话存储 | `model_provider_id` + `model` 字符串，**不**外键到目录行 id |

---

## 1. 目标与非目标

### 目标

1. 用户在 Web 聊天中为当前会话选择平台目录里的模型；之后该会话每轮推理用所选供应商凭证 + 模型 id。
2. 未选择时行为与现网一致：用 Agent 的 `model_config`。
3. 管理员可登记中转站和直连厂商、同步或手工维护目录。
4. 同一会话从企微 / Gateway 续聊时使用 Web 已选模型，无需渠道 UI。

### 非目标（一期不做）

- 改造 Agent 表单的手填 `provider` / `base_url`（默认模型仍走现有字段）
- Ollama 进入平台目录
- 每 Agent 模型白名单、按角色限制可选模型
- 聊天框自由输入任意模型名（只能选列表项）
- 流式进行中更换模型并作用于当前轮
- 按工具 / 技能自动选模型
- 改 `chat.proto` 发消息体
- 渠道侧模型下拉
- 独立「模型管理员」角色（与现有能创建/编辑 Agent 的写权限对齐）
- 用量计费、按模型配额

---

## 2. 现状（实现必须对齐）

| 已有 | 缺口 |
|------|------|
| Agent `model_config`：`provider` / `model` / `api_key` / `base_url` / `max_output_tokens` | 每 Agent 手填，无平台目录 |
| `NewModelFromConfig`：`openai`（支持 BaseURL）、`dashscope`、`ollama` | 中转站只能靠手填 base_url，无同步、无会话覆盖 |
| 每轮 `SendMessage*` 从 Agent 组模型 | 无法按会话换模型 |
| `/runtime/v1/turns` 同样从 Agent 组模型 | 渠道无法吃会话覆盖 |

`api_key` 存储方式与现有 Agent `model_config.api_key` 相同（JSON 字段；UI 已按密钥处理）。供应商表沿用同一套，读 API 不回明文，只给 `has_api_key`。

---

## 3. 架构

```
管理台「模型供应商」
    │  CRUD provider + sync + catalog 编辑
    ▼
model_providers  ──<  model_catalog（缓存）
    │
ChatPage 下拉  GET catalog?usable=1
    │  PATCH /sessions/{id}/model
    ▼
chat_sessions.model_provider_id + model   （空 = Agent 默认）
    │
SendMessage / Stream / runtime turns
    │  有覆盖：provider 行 → ModelConfig → NewModelFromConfig
    │  无覆盖：Agent.model_config
    ▼
OpenAI 兼容 HTTP 或 DashScope 客户端
```

**动态绑定**：Agent 实体、工具、workspace 不变。每轮根据会话列现场创建模型客户端。换下拉只影响 **下一句**；当前流式轮次用开始时已绑定的客户端。

**Fork**：创建子会话时拷贝父会话的 `model_provider_id` 与 `model`（与「会话状态」一致）。Rewind 不改这两列。

---

## 4. 数据

### 4.1 `model_providers`

| 列 | 说明 |
|----|------|
| id | UUID |
| name | 展示名 |
| kind | `openai_compat` \| `dashscope` |
| base_url | openai_compat 必填（官方可用 `https://api.openai.com/v1`）；dashscope 可空 |
| api_key | 与 Agent 模型 key 同等存储；可空则该供应商不可用于发消息 |
| enabled | 默认 true |
| created_at / updated_at | |

`kind=openai_compat` 覆盖：官方 OpenAI 与 One API / New API 类中转站（差异只在 `base_url` + key）。组 `ModelConfig.Provider` 时写 `openai`。

`kind=dashscope` 组 `Provider=dashscope`，走现有 DashScope 客户端；**不支持** `/v1/models` 同步。

### 4.2 `model_catalog`

| 列 | 说明 |
|----|------|
| id | UUID |
| provider_id | FK providers |
| model | 上游模型 id（发请求用） |
| display_name | 下拉文案，默认同 `model` |
| hidden | 默认 false |
| source | `sync` \| `manual` |
| display_name_overridden / hidden_overridden | 或等价：同步时若用户改过则不覆盖这两项 |
| unique | `(provider_id, model)` |

删除供应商时级联删除其目录行。已指向该 provider 的会话覆盖 **不级联清空**；发消息按 §7 报 400。

### 4.3 `chat_sessions` 新增

- `model_provider_id` 可空（无 FK 强制，避免删供应商时更新全部会话；逻辑上应对应仍存在的 provider）
- `model` 可空字符串

两者都空 = Agent 默认。只填一个视为非法，PATCH 拒绝；读到历史脏数据时发消息 400。

`max_output_tokens` 一期仍只来自 **Agent** `model_config`，覆盖只换供应商与模型名。

---

## 5. API

原始 HTTP，挂在 Portal `/api/v1`，风格对齐 Rewind/Fork，**不改** proto。

### 5.1 管理（写权限 = 现有可 Create/Update Agent 的调用方）

- `GET/POST /api/v1/model-providers`
- `GET/PATCH/DELETE /api/v1/model-providers/{id}`  
  PATCH 省略 `api_key` 表示不改 key；空字符串表示清空。GET 单条/列表：`has_api_key: bool`，无明文。
- `POST /api/v1/model-providers/{id}/sync`  
  `openai_compat`：`GET {base_url}/models`（OpenAI 列表语义）。按 `model` upsert：新行 `source=sync`；已存在则更新（但不改 `display_name`/`hidden` 若已被手工覆盖）。上游消失的模型 **不自动删**（避免会话指向的名字从目录消失；管理员可隐藏）。  
  `dashscope`：400，文案要求手工添加。
- `GET/POST /api/v1/model-catalog`（POST 手工加：`provider_id` + `model` + 可选 `display_name`）
- `PATCH/DELETE /api/v1/model-catalog/{id}`（隐藏、展示名；DELETE 仅手工行或管理员强制删）

测连通：一期可用 sync 成败代替；不单独做 `/test` 也可。若实现 `/test` 只打最小 GET，非必须。

### 5.2 聊天

聊天只读列表固定为：

`GET /api/v1/agents/{agent_id}/model-choices`

需对该 Agent 有会话读权限（与 `ListSessions` / 进 ChatPage 相同）。**不要**复用管理端 `GET /api/v1/model-catalog`（管理列表含隐藏项与密钥标记，ACL 是 Agent 写权限）。

返回：

- `{ "id": "agent_default", "label": "Agent 默认（{provider}/{model}）" }`
- 其余 usable 目录项：供应商 `enabled`、**有 api_key**、条目 `!hidden`。字段：`provider_id`、`provider_name`、`model`、`display_name`、`kind`

无 key 的供应商不进下拉（与发消息 400 对齐，避免可选却一发就失败）。

当前会话覆盖：现有 `GET /api/v1/sessions/{id}`（或 Web 已用的 getSession）增加 `model_provider_id`、`model`（空表示默认）。ChatPage 在 `sessionId` 变化时用该读接口还原下拉，不依赖 PATCH 后的本地 state，也不改 `SendMessage` proto。

- `PATCH /api/v1/sessions/{id}/model`  
  ACL 与发消息相同（会话所属用户）。  
  Body：
  - `{ "choice": "agent_default" }` 或 `model_provider_id=null` 且 `model=""` → 清空覆盖
  - `{ "model_provider_id", "model" }` → 必须命中一条 usable 目录项（同供应商 + 精确 model 字符串）

发消息 RPC / `/runtime/v1/turns` **请求体不增加模型字段**。

---

## 6. 发消息组模型

在现有 `BuildReActAgent` / runtime `startStreamTurn` 取 Agent 之后：

```
if sess.ModelProviderID != "" && sess.Model != "" {
  p := loadProvider(sess.ModelProviderID)
  if p == nil || !p.Enabled || p.APIKey == "" { return 400 MODEL_PROVIDER_UNAVAILABLE }
  if !catalogUsable(p.ID, sess.Model) { return 400 MODEL_CHOICE_UNAVAILABLE }
  cfg = ModelConfig{ Provider: factoryProvider(p.Kind), Model: sess.Model, APIKey: p.APIKey, BaseURL: p.BaseURL }
  // max_output_tokens 仍从 Agent.model_config 合并
} else {
  cfg = Agent.model_config
}
NewModelFromConfig(cfg)
```

`factoryProvider`：`openai_compat` → `"openai"`；`dashscope` → `"dashscope"`。

400 时 **不** 自动清空会话覆盖，前端提示重选。

---

## 7. 错误

| 条件 | 码 |
|------|------|
| PATCH 模型不在 usable 列表 | 400 `MODEL_CHOICE_INVALID` |
| PATCH 会话无权限 / 不存在 | 与现有 GetSession 相同 |
| 发消息时供应商缺失、禁用、无 key | 400 `MODEL_PROVIDER_UNAVAILABLE` |
| 发消息时覆盖模型已隐藏或已不在目录 | 400 `MODEL_CHOICE_UNAVAILABLE` |
| 覆盖两列只填一个 | 400 `MODEL_CHOICE_INVALID` |
| sync 上游失败 | 502/400，目录保持上次成功 |
| dashscope 调 sync | 400 |

上游推理错误仍按现网透出到聊天。

---

## 8. 前端

- **ChatPage**：输入区上方或标题旁 `<select>`，分组（Agent 默认 / 各供应商）。`sessionId` 变化后拉 choices + 当前会话覆盖。变更立即 PATCH。`streaming` 时 disabled。
- **管理页**：供应商列表与表单、同步按钮、目录表（隐藏开关、展示名、手工添加）。不在 ChatPage 暴露供应商 CRUD。

文案：下拉无模型时仍显示「Agent 默认」，管理台空目录提示去同步或手工添加。

---

## 9. 测试

**后端**

- 无覆盖：组模型参数等于 Agent `model_config`（含 base_url/key）。
- 有覆盖：`Provider/Model/APIKey/BaseURL` 来自供应商 + 所选 model；`max_output_tokens` 仍来自 Agent。
- PATCH 非目录模型 → 400；合法 PATCH 后 SendMessage 使用新 cfg。
- 供应商禁用后：会话列不变，SendMessage 400。
- sync：新模型插入；已改 display_name 的行不被上游名字覆盖；上游删除不自动删本地行。
- dashscope sync → 400。
- runtime `turns` 不传模型字段时，有覆盖则用覆盖（service 单测即可）。

**前端**

- 选项含 Agent 默认 + 分组目录；streaming disabled。
- 选中后 PATCH；失败时 UI 不假装已切换。

一期不强制渠道 e2e。

---

## 10. 实现顺序（供计划）

1. 表与 repo：providers、catalog、session 两列  
2. 管理 HTTP + sync  
3. 组模型接入 `SendMessage*` 与 runtime turns  
4. `PATCH session/model` + usable choices  
5. Web 管理页 + ChatPage 下拉  

---

## 11. 验收

- 管理台加一个中转站，同步后聊天下拉出现模型；选中后该会话下一句请求打到该 `base_url` 且 body.model 为所选 id。
- 新开会话下拉回到 Agent 默认，打到 Agent 原配置。
- 企微向同一 session 发消息，使用 Web 已选模型（可用日志或单测断言组 cfg，不必做企微 UI）。
- 关掉供应商后，该会话下一条失败并提示，而不是静默回默认。
