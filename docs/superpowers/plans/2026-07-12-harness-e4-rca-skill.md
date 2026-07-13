# E4：RCA Investigation Skill Implementation Plan

> **For agentic workers:** Use writing-plans / execute directly. Skip git commits unless asked.

**Goal:** 用 Skill 薄封装固化 RCA 调查顺序 trace → log → code，不把流程硬编码进 ReAct。

**Architecture:** 新增 `skills_examples/skills/rca-investigation/`；Agent 通过 `load_skill` / auto-route 加载。EvidenceGate Soft 文案与本 Skill 对齐。

**非目标：** 改 ReAct 内核；自动注册到所有 Agent；G4 修复流水线。

---

## 文件

| 文件 | 职责 |
|------|------|
| Create `framework/skills_examples/skills/rca-investigation/SKILL.md` | 工作流 + allowed_tools |
| Create `.../references/evidence-contract.md` | ok / error_code / evidence_refs |
| Create `framework/skills/rca_investigation_skill_test.go` 或 skills_examples 测 | Index 可扫描到 name |
| Modify gap E4 | 已落地 |

## 验收

- [x] `skills.NewIndex` 在 skills_examples 下能列出 `rca-investigation`
- [x] SKILL 声明 jaeger_trace / es_log_query / rca_*
- [x] 不修改 react_agent 硬编码路径
