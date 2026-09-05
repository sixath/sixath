# S33 Append Learning Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无生产调用者的 `append_learning` 工具，并去掉 Skills prompt 里的假入口。

**Architecture:** `os.Stat("learnings_tools.go")` 锁定文件不存在；prompt 扫描不含 `append_learning`。

**Tech Stack:** Go（`framework/tool/skillops`、`framework/skills`）

**规格:** [`2026-09-05-append-learning-off-design.md`](../specs/2026-09-05-append-learning-off-design.md)

**分支:** 从 `feature/s32-failure-capture-off` 切 `feature/s33-append-learning-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/tool/skillops/learnings_off_test.go`、`framework/skills/prompt.go` 的锁定断言 |
| 删除 | `framework/tool/skillops/learnings_tools.go`、`learnings_tools_test.go` |
| 改 | `framework/skills/prompt.go` |

禁止：删 growth；改 skill_manage；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestLearningsToolsGoRemoved`
- [ ] `TestBuildSkillsAwarePrompt_omitsAppendLearning`
- [ ] 先跑应失败

---

### Task 2: 删工具并改 prompt

- [ ] `git rm` learnings 工具文件；删 prompt 教唆句
- [ ] `cd framework && go test ./tool/skillops ./skills ./templates -count=1`
- [ ] **Commit** `fix(skillops): drop unused append_learning after default path unwired`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
