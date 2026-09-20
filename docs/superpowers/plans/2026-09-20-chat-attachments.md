# Chat Attachments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Web 对话支持图片（当前轮多模态 `Parts`）与文本类文件（会话 `uploads/` + 脚注路径）；刷新后经附件 GET 回放缩略图。

**Architecture:** 先 `POST …/attachments` 落盘并写入 `chat_attachments` 表；发消息带 `attachment_ids`，user message metadata 拷贝描述符；组装模型时仅当前 turn 图片进 `data:` `Parts`，历史图只留脚注。修 `openAIChatMessage` 走 `MultiContent`。附件读写用自定义 HTTP（multipart / raw bytes），与现有 SSE 并存。

**Tech Stack:** Go (portal + framework/model)、MySQL/`chat_attachments`、Kratos chat proto（仅扩展 `attachment_ids`）、React ChatPage。

**Spec:** [docs/superpowers/specs/2026-09-20-chat-attachments-design.md](../specs/2026-09-20-chat-attachments-design.md)

**钉死：**
- 索引用 **`chat_attachments` 表**（禁止仅内存；不用 sidecar 作为主索引）。
- 引用集合：任意 `chat_messages.metadata.attachments[].id` 出现即算被引用 → DELETE 409。
- 仅 **当前 turn 新 user message** 重建 image `Parts`；更早历史只脚注。
- **落库 content 始终含图片脚注**（即使本轮有 `Parts`）。首次送模型时可另组 `modelContent`：有 Parts 则省略图片脚注；vision 降级重试必须用**带脚注**的落库 content + 无 Parts。禁止把「省略脚注」的字符串写入 DB。
- Vision：一期无可靠能力标记时，**先带 Parts；上游 modality 类错误则同轮降级重试一次**（§5.3 路径 2）。不猜模型名单。
- `content` 与 `attachment_ids` 不能同时为空；允许仅附件。
- 渠道 / Gateway：**不接线**。
- Context budget：一期不改 snip 算法；靠上传 10MB 上限 + vision 降级；若现有 budget 忽略 `Parts` 体积，接受该诚实上限（不另开任务除非实现中炸掉）。
- 提交：仅在用户明确要求时 commit（跳过各 Task 的 commit 步，除非用户说提交）。

---

## File map

| File | 职责 |
|------|------|
| `framework/model/openai_tools.go` | `openAIChatMessage` 序列化 `Parts` → `MultiContent` |
| `framework/model/openai_tools_multimodal_test.go` | 工具路径多模态单测 |
| `portal/internal/chat/attachment.go` | 白名单、sanitize、kind、大小、脚注/`Parts` 组装纯函数 |
| `portal/internal/chat/attachment_test.go` | 上述单测 |
| `portal/internal/data/model/chat.go` | `ChatAttachment` GORM 模型 |
| `portal/internal/data/chat_attachment.go` | CRUD / CountBySession / ListByIDs |
| `portal/internal/biz/chat.go` | usecase 封装附件 + DeleteSession 清目录 |
| `portal/internal/service/chat_attachments.go` | Upload / Get / Delete HTTP 业务 |
| `portal/internal/server/chat_attachments_http.go` | 注册 `POST/GET/DELETE …/attachments` |
| `portal/api/chat/v1/chat.proto` | `SendMessageRequest.attachment_ids` |
| `portal/internal/service/chat.go` | `SendMessageStream` 校验、metadata、组装 Parts |
| `portal/internal/service/chat_attachments_test.go` / `chat_attach_assemble_test.go` | 服务层单测 |
| `web/src/api/client.ts` | upload / delete / url-for-get / sendMessageStream 带 ids |
| `web/src/pages/ChatPage.tsx` + `ChatPage.css` | composer 附件 UI + 气泡预览 |

---

### Task 1: `openAIChatMessage` 支持 Parts

**Files:**
- Modify: `framework/model/openai_tools.go`（`openAIChatMessage`）
- Create: `framework/model/openai_tools_multimodal_test.go`
- Reference: `framework/model/openai.go` `buildChatRequest`（复制 MultiContent 逻辑，勿改行为分叉）

- [ ] **Step 1: 写失败测试**

```go
package model

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestOpenAIChatMessage_WithImageParts(t *testing.T) {
	msg, err := openAIChatMessage(Message{
		Role:    "user",
		Content: "see",
		Parts: []ContentPart{
			{Type: ContentTypeText, Text: "see"},
			{Type: ContentTypeImageURL, URL: "data:image/png;base64,abc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.MultiContent) != 2 {
		t.Fatalf("MultiContent=%d", len(msg.MultiContent))
	}
	if msg.MultiContent[1].Type != openai.ChatMessagePartTypeImageURL {
		t.Fatalf("part1=%v", msg.MultiContent[1].Type)
	}
	if msg.Content != "" {
		t.Fatalf("Content should be empty when MultiContent set, got %q", msg.Content)
	}
}

func TestOpenAIChatMessage_PlainTextUnchanged(t *testing.T) {
	msg, err := openAIChatMessage(Message{Role: "user", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "hi" || len(msg.MultiContent) != 0 {
		t.Fatalf("%+v", msg)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd framework && go test ./model -run TestOpenAIChatMessage_ -count=1
```

Expected: FAIL（尚无 MultiContent 分支）

- [ ] **Step 3: 实现** — 在 `openAIChatMessage` 中，若 `len(m.Parts)>0`，按 `buildChatRequest` 同样规则填 `MultiContent`，且不设字符串 `Content`；否则保持现逻辑（含 tool_calls / reasoning）。

- [ ] **Step 4: Run — expect PASS**

```bash
cd framework && go test ./model -run TestOpenAIChatMessage_ -count=1
```

- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 2: 附件纯函数（白名单 / sanitize / 脚注）

**Files:**
- Create: `portal/internal/chat/attachment.go`, `portal/internal/chat/attachment_test.go`

- [ ] **Step 1: 写失败测试**

```go
package chat

import "testing"

func TestClassifyAttachment(t *testing.T) {
	if k, ok := ClassifyAttachment("a.PNG", "image/png"); !ok || k != KindImage {
		t.Fatalf("png: %v %v", k, ok)
	}
	if k, ok := ClassifyAttachment("a.log", "text/plain"); !ok || k != KindText {
		t.Fatalf("log: %v %v", k, ok)
	}
	if _, ok := ClassifyAttachment("a.pdf", "application/pdf"); ok {
		t.Fatal("pdf should reject")
	}
}

func TestSafeFilename(t *testing.T) {
	if got := SafeFilename(`..\..\x.png`); got == "" || stringsContainsDotDot(got) {
		t.Fatalf("%q", got)
	}
}

func TestBuildUserContentWithAttachments(t *testing.T) {
	// persist=true：始终含图片脚注（落库 / 降级重试）
	persisted := BuildUserContentWithAttachments("hello", []AttachmentMeta{
		{Kind: KindImage, RelativePath: "sessions/s1/uploads/att_1_a.png"},
		{Kind: KindText, RelativePath: "sessions/s1/uploads/att_2_a.log"},
	}, ContentForPersist)
	if !stringsContains(persisted, "att_1_a.png") || !stringsContains(persisted, "att_2_a.log") {
		t.Fatalf("persist: %q", persisted)
	}
	// forModelWithParts：可省略图片脚注，文本脚注仍在
	forModel := BuildUserContentWithAttachments("hello", []AttachmentMeta{
		{Kind: KindImage, RelativePath: "sessions/s1/uploads/att_1_a.png"},
		{Kind: KindText, RelativePath: "sessions/s1/uploads/att_2_a.log"},
	}, ContentForModelWithImageParts)
	if stringsContains(forModel, "〔附件 image〕") {
		t.Fatalf("model with parts should omit image footnote: %q", forModel)
	}
	if !stringsContains(forModel, "att_2_a.log") {
		t.Fatalf("text footnote required: %q", forModel)
	}
}
```

（测试里用 `strings.Contains`；实现导出 `ClassifyAttachment`、`SafeFilename`、`KindImage`/`KindText`、`AttachmentMeta`、`ContentForPersist` / `ContentForModelWithImageParts` 模式常量、`BuildUserContentWithAttachments(content, metas, mode)`、`BuildImageParts(metas, readFile)`。）

- [ ] **Step 2: Run FAIL**

```bash
cd portal && go test ./internal/chat -run 'TestClassify|TestSafe|TestBuildUser' -count=1
```

- [ ] **Step 3: 实现** — 常量：图 10MB、文本 2MB、每 session 20；扩展名白名单按 spec §4.1；脚注格式按 §5.1。

- [ ] **Step 4: Run PASS**

- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 3: `chat_attachments` 表 + data/biz

**Files:**
- Modify: `portal/internal/data/model/chat.go` — 加 `ChatAttachment`
- Create: `portal/internal/data/chat_attachment.go`
- Modify: `portal/internal/biz/chat.go` — 附件 usecase 方法
- Modify: AutoMigrate 注册处（搜现有 `ChatMessage` migrate，一并挂上）

**模型字段（钉死）：**

```go
type ChatAttachment struct {
	ID           string    `gorm:"column:id;primaryKey;size:36"`
	SessionID    string    `gorm:"column:session_id;size:36;index;not null"`
	Kind         string    `gorm:"column:kind;size:16;not null"` // image|text
	Mime         string    `gorm:"column:mime;size:128;not null"`
	Name         string    `gorm:"column:name;size:256;not null"`
	Size         int64     `gorm:"column:size;not null"`
	RelativePath string    `gorm:"column:relative_path;size:512;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}
```

- [ ] **Step 1: 写 data 层测试**（可用 sqlite 或现有 test DB 模式；若项目无 DB 测试基建，则测 biz 用 fake repo interface）

优先：在 `biz` 定义小接口：

```go
type AttachmentRepo interface {
	Create(ctx context.Context, a *ChatAttachment) error
	Get(ctx context.Context, sessionID, id string) (*ChatAttachment, error)
	Delete(ctx context.Context, sessionID, id string) error
	CountBySession(ctx context.Context, sessionID string) (int64, error)
	ListByIDs(ctx context.Context, sessionID string, ids []string) ([]*ChatAttachment, error)
}
```

单测用内存 fake。

- [ ] **Step 2–4: TDD 实现 Create/Get/Delete/Count/ListByIDs + AutoMigrate**

- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 4: 上传 / GET / DELETE HTTP

**Files:**
- Create: `portal/internal/service/chat_attachments.go`
- Create: `portal/internal/server/chat_attachments_http.go`
- Wire: `portal/internal/server/http.go`（或现有 NewHTTPServer）注册三条路由
- Test: `portal/internal/service/chat_attachments_test.go`（httptest 或 service 单测）

**路由（钉死）：**

```text
POST   /api/v1/sessions/{session_id}/attachments
GET    /api/v1/sessions/{session_id}/attachments/{att_id}
DELETE /api/v1/sessions/{session_id}/attachments/{att_id}
```

**Upload 流程：**
1. 鉴权 + 加载 session → agent → `workspace` 非空否则 400「请先配置 Agent workspace」
2. `ParseMultipartForm`，字段 `file`
3. `ClassifyAttachment` + 大小；`CountBySession` < 20
4. 生成 `att_id`（uuid）、`SafeFilename`、写 `{workspace}/sessions/{sid}/uploads/{att_id}_{name}`（`MkdirAll` 0755）
5. `AttachmentRepo.Create`
6. JSON 200：id/kind/mime/name/size/relative_path

**GET：** 查表 → `os.ReadFile` 绝对路径 → `Content-Type: mime`，`Content-Disposition: inline`

**DELETE：** 若任意消息 metadata 引用该 id → 409；否则删文件 + 删行 → 204

引用检测 helper：

```go
func MessageReferencesAttachment(messages []biz.ChatMessage, attID string) bool
```

扫 `metadata["attachments"]` 数组里的 `id`。

- [ ] **Step 1: 写失败测试**（upload 拒绝 pdf；无 workspace 400；delete 被引用 409）

- [ ] **Step 2: Run FAIL → Step 3 实现 → Step 4 PASS**

- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 5: Proto `attachment_ids` + SendMessageStream 接线

**Files:**
- Modify: `portal/api/chat/v1/chat.proto` — `SendMessageRequest` 增加 `repeated string attachment_ids = 3;`
- Run: `cd portal && make api`（生成 pb）
- Modify: `portal/internal/service/chat.go` — `SendMessageStream`（及非流式 `SendMessage` 若仍使用）

**校验（钉死）：**

```go
ids := uniqueNonEmpty(req.GetAttachmentIds())
if strings.TrimSpace(content) == "" && len(ids) == 0 && ir == nil && cr == nil {
  return error 400 // 与今天 empty 检查对齐并扩展
}
```

HITL（`input_response` / `confirm_response`）路径：**忽略附件**（一期不绑）。

**正常 turn：**
1. `ListByIDs`；缺 id 或文件不存在 → 400，不写消息（**已绑定 id 允许再次引用**——只要行仍在 `chat_attachments` 且文件在盘上）
2. `persistContent := BuildUserContentWithAttachments(..., ContentForPersist)` — **始终含图片脚注**
3. `CreateMessageWithMetadata(user, persistContent, metadata{attachments: [...]})`
4. `modelContent := BuildUserContentWithAttachments(..., ContentForModelWithImageParts)` + `Parts := BuildImageParts(...)`
5. 组装 history：历史行用 DB content（已有脚注）、**不**填 Parts；当前 user 用 `modelContent` + `Parts`
6. 调 ReAct；若 vision 降级：用 `persistContent` + 无 Parts 再跑一次

抽出可测函数到 `portal/internal/chat/attachment.go`：

```go
func AssembleTurnMessages(history []model.Message, currentUser model.Message) []model.Message
// currentUser 含 Parts；history 中旧 user 若误带 Parts 则 StripImageParts
```

- [ ] **Step 1: 单测** `TestAssembleTurnMessages_OnlyCurrentHasParts`、`TestSendRejectsEmptyContentAndAttachments`、`TestAlreadyBoundAttachmentIDReusable`（ListByIDs 返回已引用过的 id 仍 OK）

- [ ] **Step 2–4: TDD 实现 + `make api` + 跑测**

```bash
cd portal && make api && go test ./internal/chat ./internal/service -count=1 -run 'Attachment|Assemble|SendMessage|Reusable'
```

**DI 接线：** 在现有 `data.NewChatRepo` / `biz.NewChatUsecase` / `service.NewChatService` / `server.NewHTTPServer` 同一条 Provider 链挂上 `AttachmentRepo` 与附件 HTTP 注册（仿 `ChatMessage` 迁移注册方式）。
- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 6: Vision 降级（同轮一次）

**Files:**
- Modify: `portal/internal/service/chat.go` 或 stream runner 错误路径
- Test: 模拟上游错误字符串含 `image`/`vision`/`modality`/`not support` 时，去掉 Parts、保留脚注、重跑 **一次**，SSE/日志 `attachment_vision_degraded=true`

- [ ] **Step 1: 单测** `ShouldDegradeVisionError(err error) bool` + 「最多重试一次」状态机：首次用 modelContent+Parts，降级后必须用 **persistContent、Parts=nil**

- [ ] **Step 2–4: 实现并接入 Stream 失败分支（仅附件 vision 场景）**

- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 7: 删会话清理 uploads

**Files:**
- Modify: `portal/internal/service/chat.go` `DeleteSession` 或 `biz.ChatUsecase.DeleteSession`
- 在删 DB 前：解析 agent workspace，`os.RemoveAll(filepath.Join(workspace, "sessions", sessionID))`；错误只打日志不阻断删库（避免脏 workspace 导致删不了会话）
- 同时 `DELETE FROM chat_attachments WHERE session_id=?`（若 FK 未级联）

- [ ] **Step 1: 单测** fake fs 或 temp dir：删 session 后 uploads 目录不存在

- [ ] **Step 2–4: 实现**

- [ ] **Step 5: Commit**（仅用户要求时）

---

### Task 8: Web API 客户端

**Files:**
- Modify: `web/src/api/client.ts`

```ts
export type ChatAttachment = {
  id: string
  kind: 'image' | 'text'
  mime: string
  name: string
  size: number
  relative_path: string
}

// chatApi.uploadAttachment(sessionId, file): Promise<ChatAttachment>
// chatApi.deleteAttachment(sessionId, attId): Promise<void>
// chatApi.attachmentURL(sessionId, attId): string  // 带 token 的话用 blob fetch；若仅 cookie/header，历史缩略图用 fetch+blob URL

// sendMessageStream(..., content, callbacks, { attachment_ids?: string[] })
```

鉴权：复用 `authHeaders()`；upload 用 `FormData`，**不要**手动设 `Content-Type`（让浏览器带 boundary）。

缩略图：`fetch(GET)` + `blob()` + `URL.createObjectURL`（因需要 Authorization header，不能直接 `<img src>` 裸链，除非改成 query token——一期用 blob URL）。

- [ ] **Step 1: 改 client 类型与方法**
- [ ] **Step 2: Typecheck** `cd web && npx tsc --noEmit`（或项目现有脚本）
- [ ] **Step 3: Commit**（仅用户要求时）

---

### Task 9: ChatPage composer + 气泡

**Files:**
- Modify: `web/src/pages/ChatPage.tsx`, `web/src/pages/ChatPage.css`

**行为钉死：**
- 回形针 `<input type="file" multiple accept=".png,.jpg,.jpeg,.webp,.gif,.txt,.log,.md,.json,.csv,image/*">`
- 拖拽到 composer；`paste` 若 `clipboardData.files` 有图则 upload
- 待发列表 state：`pendingAttachments: ChatAttachment[]`；移除时 `deleteAttachment`（忽略 409）
- 发送：`trim(content)||pending.length`；`attachment_ids: pending.map(a=>a.id)`；发送成功后清空 pending
- 上传中：`uploadingCount>0` 禁用发送
- 历史：user message `metadata.attachments` → image 用 blob 缩略图；text 显示文件名，点击下载

- [ ] **Step 1: 实现 UI（可先无 e2e）**
- [ ] **Step 2: 浏览器手测清单**（localhost Vite）
  - 纯文本仍可用
  - 选 png → chip → 发送 → 气泡图
  - 选 log → 脚注路径出现在消息
  - 粘贴图
  - 刷新后缩略图仍在
  - 无正文仅附件可发
- [ ] **Step 3: Commit**（仅用户要求时）

---

### Task 10: 回归与收尾

- [ ] **Step 1: 跑相关测试**

```bash
cd framework && go test ./model -count=1
cd portal && go test ./internal/chat ./internal/service -count=1
```

- [ ] **Step 2: 确认渠道代码未改**（`gateway/` 无 attachment 逻辑）
- [ ] **Step 3: 若有 `web/e2e`，补一条 mock 上传+发送（可选；无则手测即可）
- [ ] **Step 4: 对照 spec §8 验收打勾**

---

## 风险与注意

| 风险 | 处理 |
|------|------|
| `CreateMessage` 仍要求非空 content | 允许脚注-only content；DB 存脚注文本 |
| pathguard 拒 `sessions/...` | 确认相对路径在 workspace 内；必要时加测试 |
| 大图 base64 撑爆请求 | 上传 10MB 硬顶；context 超限走降级 |
| 非流式 `SendMessage` | 若仍暴露，同样接 `attachment_ids` 或文档标明仅 stream |

---

## 执行交接

Plan 写完并经 plan-document-reviewer ✅ 后，向用户提供两种执行方式（见 skill）。
