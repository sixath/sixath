# Knowledge Write（draft → approve）设计

**日期**: 2026-08-08  
**状态**: 已确认（2026-08-08）；实现计划见 `docs/superpowers/plans/2026-08-08-knowledge-write.md`  
**关联**: [`2026-08-07-memory-hub-governance-knowledge-plugins-design.md`](./2026-08-07-memory-hub-governance-knowledge-plugins-design.md) §4.1 / §4.2 / §5  
**动机**: Agent 当前仅有 `knowledge_search` / `knowledge_read`；无法把分析结论写回 Wiki / units，只能口头「手动搬运」。

---

## 1. 目标与非目标

### 目标（一期）

1. Agent 可通过工具把内容写入 **本地 Wiki**（主路径）与 **memory units**（可选路径）。
2. **一律先 draft，再 approve 成 active**；默认召回 / `knowledge_search` **不包含 draft**。
3. **两条审批入口**：
   - Agent 工具 `knowledge_approve`
   - Portal UI（Hub 资产审批，复用同一后端语义）
4. 路径与权限 fail-closed：禁止目录穿越；无后端则明确报错。

### 非目标（一期不做）

- Confluence / 飞书 / 外部 Hub ingest
- CodeGraph 写入
- 覆盖正式页的 UI 确认卡（draft 本身即门控；覆盖 active 用 `overwrite=true`）
- 改 `memory_units.status` DB 枚举（继续用 `metadata.hub_status`，见主设计 §4.1）

---

## 2. 方案选择

采用 **方案 B**：

| 工具 | 职责 |
|------|------|
| `knowledge_write` | **只写 draft** |
| `knowledge_approve` | draft → active |

不采用「单工具多 mode」以免与 UI 审批语义纠缠；不把 units 写入挤回裸 `memory_remember`（避免「写入知识」语义分裂）。

### 2.1 审批语义（一期锁死）

一期门控是 **「draft 不进默认召回」**，不是双人审批。

- **允许** Agent 调用 `knowledge_approve`（可写后自批）；这是产品选择（工具 + UI），不是漏洞。
- UI Approve 与工具走**同一服务层**，便于人审与审计；不强制「必须人点」。
- 二期若要人审强制，再加策略（如 `require_human_approve_wiki: true`），本期不做。

---

## 3. 工具契约

### 3.1 `knowledge_write`

**输入**

| 字段 | 类型 | 说明 |
|------|------|------|
| `source` | string | 必填：`wiki` \| `units` |
| `id` | string | wiki：相对 `SATH_HUB_WIKI_ROOT` 的路径（建议 `foo/bar.md`）；units：可选，空则新建 |
| `content` | string | 必填：正文（markdown / 纯文本） |
| `title` | string | units 可选标题；wiki 可忽略（用文件名） |

**行为**

- `source=wiki`
  - 要求 `DirWiki` / wiki root 已配置，且对该 source 可写
  - 规范化 `id`：禁止 `..`、绝对路径、空 path；必须落在 root 下
  - **一期仅支持正式扩展名 `.md`**（传入 `.markdown`/`.txt`/`.mdx` → 参数错误；无扩展名则补 `.md`）
  - **Draft ↔ 正式映射**（唯一规则）：
    - 正式 `docs/foo.md` ↔ draft `docs/foo.draft.md`
    - 判定 draft 文件：`strings.HasSuffix(name, ".draft.md")`（注意 `filepath.Ext("x.draft.md")==".md"`，故 **不能** 只靠 Ext）
    - 正式 id 剥离：若 `id` 以 `.draft.md` 结尾，规范为去掉 `.draft` 段 → `*.md`
  - 若调用方已传 `*.draft.md`，写入该路径，返回的 `id` 仍是正式 `*.md`
  - 已存在同名 draft：**覆盖 draft**（仍是 draft，不碰正式页）
  - **写大小上限**：与读侧一致，`512 KiB`；超限拒绝
  - **可写判定**：root 存在且为目录即可尝试写；`os` 权限失败按错误返回（不预先 chmod 探测）
  - 返回：`{ source, id, status: "draft", path }`（`id` = 正式相对路径）
- `source=units`
  - 要求 Agent `memory_write_enabled`（或等价 MemoryStore 可写）
  - 经 `UnitWriter`（见 §4）：`Remember` / 更新正文，并设 `metadata.hub_status="draft"`
  - 若传入已有 `id`：仅当该 unit 已是 draft（或尚无 `hub_status` 且调用方明确更新 draft）时可覆盖正文；**禁止**用 write 覆盖已 active 且无 draft 标记的 unit（应先另写新 draft 或走后续 supersede）
  - Prefetch / 默认召回过滤 `hub_status=draft`（主设计 §4.1）
  - 返回：`{ source, id, status: "draft" }`

**错误**

- 未知 `source` / 空 content → 参数错误
- 该 `source` 不可写（wiki 未配置 / units 写关）→ 明确错误字符串（如 `wiki not configured` / `memory write disabled`），**不要**因另一 source 可写而静默成功
- 路径穿越 / 超限 → 拒绝

### 3.2 `knowledge_approve`

**输入**

| 字段 | 类型 | 说明 |
|------|------|------|
| `source` | string | 必填：`wiki` \| `units` |
| `id` | string | 必填：与 write 返回的正式 id 一致 |
| `overwrite` | bool | wiki：若正式页已存在，必须 `true` 才覆盖；默认 false |

**行为**

- `source=wiki`
  - 读取正式 `id` 对应的 `*.draft.md`
  - 若不存在 draft → 错误
  - 若正式 `id` 已存在且 `overwrite!=true` → 错误（提示设 overwrite）
  - 将 draft 内容写入正式路径，然后 **删除** draft 文件（best-effort；删失败记日志但仍返回 active 若正式页已写成功——实现计划里写清顺序：先写临时/正式，成功后再删 draft）
  - 返回：`{ source, id, status: "active" }`
- `source=units`（**落库语义锁死**）
  - 找到 unit；要求当前 `metadata.hub_status=="draft"`，否则错误
  - **删除** `metadata` 中的 `hub_status` 键（恢复为「无 hub_status」≡ active 可召回，与主设计 §4.1 `{∅, active}` 对齐）
  - **不**经 `GovernanceWriter.SetStatus`（一期 wiki/unit 知识写不强制登记 Asset；二期再统一）
  - 返回：`{ source, id, status: "active" }`

### 3.3 对现有读路径的影响

- `knowledge_search`（wiki）：Walk 时若 `HasSuffix(name, ".draft.md")` → **跳过**（即使 Ext 是 `.md`）
- `knowledge_read`：
  - `id` 为正式相对路径 → 读正式页；若文件不存在返回错误
  - `include_draft=true` → 若存在对应 `*.draft.md` **优先读 draft**，否则读正式页
  - 显式传入 `*.draft.md` 作为 `id` → 允许读该 draft 文件（path jail 仍生效）；返回的 Hit.ID 规范为正式 id
- units：search/prefetch 继续过滤 draft（§4.1）

---

## 4. Provider / Capabilities / UnitWriter

`LocalKnowledge.Capabilities()`：

- `Write: true` 当 **wiki 已配置或 units 写通道可用**（任一即可）
- Flags：保留 `wiki` / `code_graph`；可选 `knowledge_write: true`
- `DescribeTools`：只要 `Write==true` 就注册 `knowledge_write` / `knowledge_approve`；调用时再按 `source` 校验可用性（不可用 source → 错误，不是隐藏半套工具）

`UnitWriter`（framework 侧窄接口，Portal 实现）：

```go
type UnitWriter interface {
    WriteDraft(ctx context.Context, id, title, content string) (unitID string, err error)
    ApproveDraft(ctx context.Context, id string) error
    ListDrafts(ctx context.Context, limit int) ([]UnitDraftMeta, error) // id, title, updated_at
}
```

- 作用域：当前 Agent / Identity（与现有 memory write 相同）
- `WriteDraft`：空 `id` → 新建；非空 → 仅允许更新 draft（见 §3.1）

---

## 5. Portal API 与 UI

### 5.1 服务层（工具与 HTTP 共用）

```text
HubKnowledgeWrite(ctx, agentID, source, id, content, title) → {id, status}
HubKnowledgeApprove(ctx, agentID, source, id, overwrite) → {id, status}
HubKnowledgeListDrafts(ctx, agentID, source?) → []DraftItem
```

`DraftItem`: `{ source, id, title?, updated_at?, preview? }`

鉴权：与现有 Agent Hub / assets 相同（登录用户对 agent 有管理/使用权限）；**禁止**未鉴权扫 wiki root。

### 5.2 HTTP（一期建议，可挂在 memory hub 路由旁）

| Method | Path | 说明 |
|--------|------|------|
| `GET` | `/api/v1/agents/{agent_id}/hub/knowledge/drafts?source=wiki\|units\|` | 列 draft；`source` 空 = 两者 |
| `POST` | `/api/v1/agents/{agent_id}/hub/knowledge/approve` | body: `{ source, id, overwrite? }` |

不另开「裸写磁盘」HTTP；UI 一期只做 **列表 + Approve**（write 走 Agent 工具即可）。若后续要 UI 编辑器，再加 `POST .../write` 调同一 `HubKnowledgeWrite`。

### 5.3 UI

- Agent Detail → Hub 面板：draft 列表 + Approve（`overwrite` 勾选当正式页冲突时）
- 枚举：wiki = 扫 root `*.draft.md`；units = `UnitWriter.ListDrafts`

---

## 6. 安全与治理

| 规则 | 说明 |
|------|------|
| Path jail | 与现有 `DirWiki.Read` 相同：`pathUnderRoot`，禁 `..` |
| 写 fail-closed | 无 root / 不可写 → 错误，不静默丢弃 |
| Draft 不进默认召回 | wiki search 跳过 `*.draft.md`；units 靠删除前的 `hub_status=draft` |
| 覆盖正式页 | 必须 `overwrite=true` |
| 自批允许 | 见 §2.1 |

一期 **不强制** 登记 Governance Asset；二期再统一 `AssetRef{Kind:Wiki|Unit, Status:Draft}`。

---

## 7. 实现落点（预计）

| 层 | 改动 |
|----|------|
| `framework/memory/hub/local/wiki_dir.go` | `WriteDraft` / `ApproveDraft` / search 跳过 draft |
| `framework/memory/hub/local/knowledge.go` | 工具描述 + `Call` 分支；`Capabilities.Write` |
| units 写 | LocalKnowledge 注入可选 `UnitWriter`（Remember + hub_status），Portal 接线 |
| `portal/internal/chat/hub_knowledge_tools.go` | 注册新工具；写权限与 `memory_write_enabled` / wiki root 对齐 |
| `portal` Hub API | Approve 端点或复用 assets status API |
| 测试 | wiki draft round-trip；search 不含 draft；approve+overwrite；units draft 过滤 |
| 文档 | `portal/docs/memory-integration.md` 补一小节 |

---

## 8. 验收

1. Agent 调用 `knowledge_write(source=wiki, …)` 后，磁盘出现 `*.draft.md`，`knowledge_search` 默认搜不到该正文。
2. `knowledge_approve` 后正式 `.md` 存在，draft 清除，search 可命中。
3. 正式页已存在时无 `overwrite` → 失败；`overwrite=true` → 成功。
4. `knowledge_write(source=units)` 产生 draft unit；prefetch/默认召回不可见；approve 后可见（在 memory_write 开启时）。
5. UI Approve 与工具 approve 结果一致（同一文件 / 同一 unit 状态）。
6. 路径 `../` 等穿越被拒绝。

---

## 9. 已定默认（原开放问题）

| # | 决定 |
|---|------|
| Q1 | draft 后缀 = **`.draft.md`**；判定用 `HasSuffix`，不用 `Ext` |
| Q2 | approve 后 **不**强制写 Governance Asset（二期） |
| Q3 | 正式 id 默认读正式页；`include_draft=true` 或显式 `*.draft.md` 才读 draft |
| Q4 | units approve = **删除** `hub_status` 键 |
| Q5 | Agent **允许**自批（§2.1） |

---

## 10. 决策记录

| 项 | 选择 |
|----|------|
| 写入目标 | Wiki 为主 + units 可选 |
| 门控 | draft 不进默认召回（非双人审） |
| 审批 | 工具 + UI（可自批） |
| 工具拆分 | `knowledge_write` + `knowledge_approve` |
| units 落库 | `hub_status=draft` → approve 时删键 |
