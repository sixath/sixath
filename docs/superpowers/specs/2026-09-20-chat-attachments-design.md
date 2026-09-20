# 对话附件：图片多模态 + 会话 uploads 文本文件

> 状态：已评审（对话确认）  
> 日期：2026-09-20  
> 关联：`web/src/pages/ChatPage.tsx`、`web/src/api/client.ts`、`portal/api/chat/v1/chat.proto`、`portal/internal/service/chat.go`、`framework/model/{model,openai,openai_tools}.go`、`framework/tool/{file_tools,vision}.go`  
> 触发：Web 对话目前只能发纯文本；排障常要贴截图与日志，框架已有 `ContentPart` / `vision_analyze` / workspace 文件工具，但未接到发消息入站。

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 产品路径 | 图片 → 真正多模态（`Parts`）；文本类文件 → 落会话 uploads，消息脚注路径，Agent 用 `read_file` |
| 入口范围 | **仅 Web**；渠道（企微等）预留同名字段，一期不接线 |
| 存储 | `{agent.workspace}/sessions/{session_id}/uploads/{att_id}_{safe_name}` |
| 上传与发送 | 先 `POST …/attachments`，再 `messages/stream` 带 `attachment_ids`（方案 1） |
| 类型白名单 | 图片：`png/jpeg/webp/gif`；文本：`txt/log/md/json/csv` 等（见 §4） |
| 入模 | 图片 `data:` URL 进 `Parts`；文本文件**不**把全文塞进 prompt |
| 框架缺口 | 修 `openAIChatMessage`，使 ReAct 工具调用路径序列化 `Parts` |

一句话：Web 先上传到会话 uploads，发消息带 id；图进多模态，日志进路径脚注。

## 1. 背景

当前端到端是纯文本：

```text
ChatPage → POST /sessions/{id}/messages/stream { content }
  → DB chat_messages.content TEXT
  → history → model.Message{Role, Content}（无 Parts）
  → ReAct → openAIChatMessage 只写 Content
```

已有但未接产品面的能力：

- `framework/model.ContentPart` + `ContentTypeImageURL`
- plain `Chat`/`ChatStream` 的 `MultiContent` 路径
- `vision_analyze` / `browser_vision`（Agent 侧读 workspace 图）
- workspace `read_file` / `write_file`（需 Agent 开启 workspace 文件工具）

缺口：composer 无上传、API 无附件、历史不填 `Parts`、工具调用路径忽略 `Parts`。

## 2. 目标与非目标

### 目标（一期）

1. Web composer 支持选文件、拖拽、粘贴图片；发送前可预览并移除。
2. 图片对支持 vision 的模型以多模态形式进入 ReAct 请求。
3. 文本类文件落在会话 `uploads/`，历史刷新后仍可通过 metadata 展示；Agent 可用现有文件工具读取。
4. 无附件时行为与今天完全一致。
5. 企微等渠道零行为变化（字段仅预留）。

### 非目标（一期不做）

- 企微 / Gateway 渠道收图或收文件。
- PDF / zip 解析或内容抽取。
- 独立对象存储（S3）或跨会话附件库。
- 把文本文件全文注入 prompt。
- 改 Agent 编辑页「调试运行」或其它无关 UI。

### 诚实上限

- 多模态依赖所选模型/供应商支持 vision；不支持时降级为落盘 + 路径脚注（可选 `vision_analyze`），不能假装「模型看见了图」。
- `data:` 进请求受单图体积限制；超大图应在上传阶段拒绝，而不是打爆上游上下文。
- 文本文件可读性依赖 Agent 已启用 workspace 文件工具；未启用时脚注仍在，但工具调用会失败——需在 UI/脚注文案中提示。

## 3. 架构

```text
Composer
  选文件 / 粘贴图
    → POST /api/v1/sessions/{sid}/attachments  (multipart file)
    → 落盘 workspace/sessions/{sid}/uploads/{att_id}_{safe_name}
    → Attachment{id, kind, mime, name, size, relative_path}

  发送
    → POST …/messages/stream
         { content, attachment_ids[] }
    → 校验 ids ∈ session 且文件存在
    → CreateMessage(user)：content + metadata.attachments[]
    → 组装 model.Message：
         Content = 用户文本 + 附件脚注（文本文件路径；图片也可附路径便于工具）
         Parts   = 图片 ContentPart{Type:image_url, URL:data:…}
    → ReAct Stream（openAIChatMessage 支持 MultiContent）
```

渠道侧：`TurnRequest` / 规范化层可预留 `attachment_ids` 或等价字段，**一期不实现、不解析**。

## 4. API 与存储

### 4.1 上传

`POST /api/v1/sessions/{session_id}/attachments`

- `Content-Type: multipart/form-data`，字段名 `file`
- 鉴权：与该 session 同一用户（与发消息一致）
- 校验：
  - **图片**：扩展名/MIME ∈ `{png, jpeg/jpg, webp, gif}`；单文件 ≤ **10MB**
  - **文本**：扩展名 ∈ `{txt, log, md, json, csv}`（MIME 以 text/* 或 application/json 为主，以扩展名最终裁定）；单文件 ≤ **2MB**
  - 每 session 附件数 ≤ **20**（含已绑定消息的）
  - 文件名 sanitize：去路径分量、拒绝空名、保留安全 basename
- 响应：

```json
{
  "id": "att_…",
  "kind": "image" | "text",
  "mime": "image/png",
  "name": "error.png",
  "size": 12345,
  "relative_path": "sessions/<sid>/uploads/att_…_error.png"
}
```

`DELETE /api/v1/sessions/{session_id}/attachments/{att_id}`

- 未绑定任何消息：删文件 + 元数据，204
- 已写入某条 message metadata：409（一期不做孤儿清理策略之外的软删）

### 4.2 发消息

扩展现有 JSON（保持 SSE）：

```json
{ "content": "看看这张图和日志", "attachment_ids": ["att_1", "att_2"] }
```

- Proto：`SendMessageRequest` 增加 `repeated string attachment_ids = 3;`
- `MessageReply.metadata.attachments`：数组，元素与上传响应对齐（至少 id/kind/mime/name/relative_path），供刷新回放

**正文与附件：**

- `content` 可为空字符串，但 **`content` 与 `attachment_ids` 不能同时为空**（允许「只发图 / 只发日志」）。
- 同请求内 `attachment_ids` 去重。
- **已绑定**到历史消息的 `attachment_id` **允许**在后续消息再次引用（同一磁盘文件、多条消息 metadata 各存一份描述符拷贝）。DELETE 在**任意**消息仍引用该 id 时返回 409。
- 空 `attachment_ids`（或省略）且有非空 `content`：与今天纯文本行为一致。

绑定规则：发消息成功后，所列 attachment 计入「已被引用」集合（用于 DELETE 门禁）。

失败原子性：校验或落库失败时**不**写入半条 user message；已上传未绑定的附件仍可再次引用或 DELETE。

### 4.2.1 附件读取（一期必做）

`GET /api/v1/sessions/{session_id}/attachments/{att_id}`

- 鉴权同 session；仅返回属于该 session 的文件字节
- `Content-Type` 用存储的 mime；`Content-Disposition: inline`（图片预览）或 `attachment`（文本下载，实现可选）
- 用途：历史刷新后缩略图 / 下载；**禁止**把整图 base64 写入 `metadata`（避免撑爆消息行）

验收「刷新后缩略图可回放」依赖本 GET，**不是可选**。

### 4.3 落盘与清理

- 绝对路径：`{agent.workspace}/sessions/{session_id}/uploads/{att_id}_{safe_filename}`
- `relative_path` 相对 workspace，须能通过现有 pathguard，供 `read_file` / `vision_analyze` 使用
- 删除会话：若已有 session 目录清理钩子则一并删 `sessions/{id}/`；若无，一期在实现计划中列明确 TODO，不阻塞主路径

附件元数据一期可仅存于：

1. 上传成功后的进程内/轻量索引（或小 JSON sidecar：`uploads/.index.json`），以及  
2. 绑定后的 `chat_messages.metadata.attachments`

不强制新建独立 DB 表；若实现中发现 sidecar 竞态难处理，可改为 `chat_attachments` 表（实现计划里允许等价替换，产品语义不变）。

## 5. 入模与降级

### 5.1 组装 `model.Message`

对每个已绑定附件：

| kind | Content 脚注 | Parts |
|------|--------------|-------|
| image | 有 image `Parts` 时可省略脚注；**无** `Parts`（降级 / 模型不支持 vision）时**必填** `〔附件 image〕{relative_path}`（可提示 `vision_analyze`） | 支持 vision 时：`{Type: image_url, URL: data:{mime};base64,…}` |
| text | **必填** `〔附件 text〕{relative_path} — 可用 read_file 读取` | 无 |

用户原文与脚注之间空一行拼接。`content` 为空时允许仅脚注（或仅 Parts）。

### 5.2 `openAIChatMessage`

当 `len(Parts) > 0` 时：

- 使用 OpenAI `MultiContent`（与 plain Chat 路径一致）：text part + image_url parts
- `Content` 字符串字段按 SDK 要求置空或仅作 fallback（与现有 `buildChatRequest` 行为对齐）

无 `Parts` 时行为不变（含 tool_call_id / reasoning_content）。

### 5.3 Vision 降级

推荐顺序：

1. 若已知当前会话选用模型不支持 vision（配置或能力标记）：发送组装时不填 image `Parts`，仅脚注路径；文案可提示可用 `vision_analyze`（若该 Agent 已启用）。
2. 若未知：先带 `Parts` 请求；若上游明确返回 modality/不支持类错误，**同轮降级重试一次**（去掉 image Parts，保留脚注），并在 SSE/日志中标记 `attachment_vision_degraded=true`（勿对用户刷屏）。

不支持无限重试。

## 6. Web UI

- Composer：回形针按钮 + 拖拽命中区 + `Ctrl/Cmd+V` 粘贴图片（从 clipboard 构造 `File` 走同一上传 API）
- 发送前：缩略图（image）/ 文件名 chip（text），可单独移除（触发 DELETE 或仅从待发列表去掉；未绑定的建议 DELETE 以免占配额）
- 历史气泡：image 缩略图（`GET …/attachments/{id}`）；text 显示文件名，可点下载/打开（同一 GET）
- 发送按钮：无正文且无待发附件时禁用；仅有附件或仅有正文均可发送
- 加载态：上传中须等全部 upload settle 后再发送
- 无附件：UI 与协议与今天一致

## 7. 错误语义

| 场景 | HTTP / 行为 |
|------|-------------|
| 类型不在白名单 | 400，明确文案 |
| 超大小 / 超数量 | 400 |
| attachment_id 不属于 session 或文件缺失 | 发消息 400，不写 user message |
| 未绑定删除 | 204 |
| 已绑定删除 | 409 |
| vision 上游拒绝 | 降级重试一次（§5.3） |

## 8. 测试与验收

### 单测

- 上传白名单 / 大小 / 文件名 sanitize
- 发消息归属校验与 metadata 形状
- 历史→`model.Message`：图进 Parts、文本仅脚注
- `openAIChatMessage`：有 Parts → MultiContent；无 Parts 回归
- DELETE 绑定前后

### e2e（Web）

- 选图 → chip → 发送 → 气泡缩略图；SSE 正常结束
- 上传 `.log` → 发送 → 脚注含相对路径；组装结果无全文
- 粘贴图片 → 等同选图
- 纯文本回归

### 验收标准

1. Web 可附加图片与文本类文件并发送  
2. 支持 vision 的模型收到 image `Parts`  
3. 文本文件在 `sessions/<id>/uploads/`，可用现有文件工具读  
4. 刷新后 metadata + 经附件 GET 的缩略图/下载可回放  
5. 渠道行为不变  
6. 允许仅附件、无正文发送；已绑定附件可被后续消息再次引用 

## 9. 实现触及面（指导，非排期）

| 层 | 文件（预期） |
|----|----------------|
| Proto / HTTP | `portal/api/chat/v1/chat.proto`、chat HTTP 注册、multipart 上传 + 附件 GET |
| Service | `portal/internal/service/chat.go`（上传、读取、组装 Parts、metadata） |
| Model bridge | `framework/model/openai_tools.go`（`openAIChatMessage`） |
| Web | `ChatPage` composer、`client.ts` upload/GET + sendMessageStream |
| 清理 | session 删除钩子清理 `sessions/{id}/`（若现网无钩子则计划中单列任务） |

**Workspace 前置：** 上传要求 Agent 已配置非空 `workspace`；否则上传 API 返回 400（文案：请先配置 Agent workspace）。未启用 workspace 文件工具时仍允许上传与多模态；文本脚注保留，但 `read_file` 可能不可用——脚注文案不假装工具一定存在。

**GIF：** 白名单包含 gif；若上游不支持该 modality，走 §5.3 降级，不单独剔除 gif。

## 10. 决议记录

- 用法：**C**（图片多模态 + 文件 workspace）
- 入口：**C**（先 Web，渠道预留）
- 存储：**A**（会话 uploads）
- 类型：**B**（图片 + 文本类）
- 实现路径：**方案 1**（Upload API + `attachment_ids`）
- §1–§3 设计对话确认：通过（2026-09-20）
