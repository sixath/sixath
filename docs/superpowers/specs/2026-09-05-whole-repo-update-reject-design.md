# S6 收口：Update 拒绝整仓 workspace

**日期**: 2026-09-05  
**状态**: 已确认（父规格 §4 / S5 后续任务；2026-09-05 实施）  
**范围**: 关掉 S5「Update 不拦」waiver。不改 ReAct、PromptBuilder、RCA 注册。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S5](./2026-09-05-whole-repo-run-reject-design.md)

**一句话**: 编辑 Agent 也不能再把 workspace 写成代码根下的整仓路径；留空则落到默认可写根。

---

## 1. 背景

S5 关掉了 Run 入口。Create 早已拒绝整仓。Update 仍原样写入 `workspace` 列，Web 编辑表单每次保存都会带上当前路径，等于还能把整仓写回去。

S5 当时不拦 Update，是为了让用户能把路径改离 code root。本切片把这条写入口收成与 Create 相同的规则，并用「空字符串 → 默认可写根」作为迁移手段。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Update `workspace` 落在任一 `code_roots` 下 | `WORKSPACE_WHOLE_REPO_RETIRED` |
| Update `workspace` 为空（含只空白） | `{data_root}/agents/{id}` + `MkdirAll`（与 Create 相同） |
| Update 省略 `workspace` 字段 | **不改**该列（即使库里仍是整仓） |
| 未点保存的已有行 | **不改库**、不自动 `LinkCode` |
| Web 编辑态整仓 | 保存时发空 workspace，避免把旧路径再写回去 |
| Get / List / 建会话 | 仍不拦 |

---

## 3. 行为

```text
UpdateAgent:
  workspace 未出现 → 不碰该列
  TrimSpace 后空 → 默认可写根 + MkdirAll
  WorkspaceUnderCodeRoots → ErrWorkspaceWholeRepoRetired
  否则写入该路径
```

Web：`retiredWholeRepo` 为真时，submit 的 `workspace` 发 `""`（随后可选 `workspace-link` 仍按所选目录挂 `code/`）。

---

## 4. 非目标

- 不扫库批量改已有行
- 不自动 `LinkCode`
- 不拦省略 workspace 的 Update（改名、改 prompt 仍可）
- 不改 harness / RCA `MergeRCARoots`

---

## 5. 成功标准

1. Update 整仓路径 → `WORKSPACE_WHOLE_REPO_RETIRED`。
2. Update `workspace=""` → 目录为 `{data_root}/agents/{id}` 且存在。
3. Update 不带 workspace 字段 → 列不变。
4. `cd portal && go test ./internal/service ./internal/biz -count=1` 绿（skip 预存 SQLITE_BUSY 若碰到）。
