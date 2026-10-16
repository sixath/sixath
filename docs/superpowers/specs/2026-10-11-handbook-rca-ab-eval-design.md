# Handbook RCA 定位 A/B 评测设计

> 目标：回答「引入 handbook 后，agent 的代码排障定位能力是否变强」，产出可对比的跑分（基线 off vs 对比 on）。

## 背景

设计文档 §13.2（RCA 定位 A/B）是 P3 未实现项。本次把它落地为最小可用闭环：

- **handbook** 不是工具，而是一组 skill 目录：`HandbookUsecase.SkillDirsForAgent(agentID)` 返回
  `[code-map 入口 skill] + [各仓库 handbook skill 目录]`，经 `appendHandbookDirs` 拼进
  `extraSkillDirs` → `agent.WithReActSkillsDirs` 注入。
- 路由入口是 `code-map` skill（`summary_pinned: true` 自动注入），写着：先 `skill_view(仓库 handbook)`，
  再用 `rca_read` 核实源码。
- `rca_read`/`rca_grep`/`rca_symbol` 是独立工具（走 `RCARoots`），与 handbook 正交。

## 决策记录

| 维度 | 决策 |
|------|------|
| 路线 | A（真实仓库） |
| 判据 | 现有 `expect.root_cause` 关键词命中（`evaluateInvestigation` 已支持，零改动 judge） |
| 样本 | 用户另提供真实故障样本，第一批 3~5 题跑通 |
| 仓库访问 | portal live 模式（真仓库 + 现场生成真实 handbook） |
| off 态开关 | **环境开关**（运行时包级变量，非持久化字段） |
| agent 组织 | 同 agent，切环境变量重跑 portal 实现 off/on 对比 |

## 实现方案

### 1. portal 侧：`handbook_enabled` 环境开关

仿现有 `AllowScriptExecution` 模式（包级变量 + `Set*` 函数 + main 读环境变量）。

**文件与改动：**

- `portal/internal/chat/agent_builder.go`：新增包级变量与设置函数

```go
// HandbookEnabled 控制是否为 agent 注入 handbook skill 目录（code-map + 各仓库 handbook）。
// 默认 true；A/B 评测的 off 组通过 SATH_HANDBOOK_ENABLED=false 关闭。
var HandbookEnabled = true

func SetHandbookEnabled(enabled bool) {
	HandbookEnabled = enabled
}
```

- `portal/internal/service/handbook_dirs.go`：`appendHandbookDirs` 读开关，false 时直接返回原 dirs

```go
func appendHandbookDirs(ctx context.Context, r HandbookSkillDirResolver, agentID string, dirs []string, logger *log.Helper) []string {
	if !chat.HandbookEnabled {
		return dirs
	}
	// ... 原有逻辑不变
}
```

- `portal/cmd/backend/main.go`：启动时读环境变量（默认 true）

```go
// handbook skill 目录注入开关：环境变量 SATH_HANDBOOK_ENABLED > 默认 true
if v := os.Getenv("SATH_HANDBOOK_ENABLED"); v != "" {
	chat.SetHandbookEnabled(v == "1" || v == "true" || v == "yes")
}
```

> 注意：`service` 包需 import `chat` 包读取 `chat.HandbookEnabled`。若存在 import 环（service ↔ chat），
> 需把变量放到 `service` 包自身（或一个无环依赖的公共包）。实现时先确认依赖方向，选无环位置。

### 2. evals 侧：任务集 + runner 扩展 + 对比跑分流程

**现状约束（已核实）**：portal 模式（`runPortal`，`evals/runner/portal.go:399`）当前**只跑 `answer_shape` 任务**（`if t.Category == "answer_shape"` 硬编码），且评估只判 timeout/failed/hitl，**不判 `root_cause`**。因此要支持 rca_handbook 走 portal 模式，需扩展 runner。

**任务集**：新增 `evals/tasks/rca_handbook.jsonl`，**新 category `rca_handbook`**（不污染现有 investigation 语义）。
每题 = `input`（故障描述）+ `expect.root_cause`（关键词，`|` 分隔多选）。不用 `must_contrast`/`forbid_root`/`fixtures`（portal 模式读真实仓库，无需合成证据）。

**runner 扩展**：
1. `ValidCategories` 加 `"rca_handbook": true`（`evals/runner/task.go:126`）；`Validate` 加 `case "rca_handbook"`：只要求 `root_cause` 非空（不要求 fixtures）。
2. `runPortal`：除 `answer_shape` 外也挑出 `rca_handbook` 任务（`evals/runner/portal.go:400-405`）。
3. `runPortalTask`：对 `rca_handbook` 任务，正常答复时用 `evaluateInvestigation` 判 `root_cause` 命中（`investigationEvidence{Output: turn.Answer, ToolCalls: 0, ContrastHits: 0}`），填 `TaskResult.Passed` / `FailureReason`（现在空答复直接返回空 TaskResult，pass/fail 未填）。

**A/B 跑分流程（portal 模式）**：
1. **off 基线**：`SATH_HANDBOOK_ENABLED=false` 启动 portal → `runner -mode portal -agent <id> -tasks evals/tasks/rca_handbook.jsonl`
2. 触发 handbook 生成（cron `RebuildStale` 或 `RequestRebuild`），等 ready
3. **on 对比**：默认 portal → 同样 runner 命令
4. 对比两次结果的命中率 / 失败原因分布 / token 消耗

### 3. 指标（本阶段）

- 命中率（`root_cause` 关键词命中通过率）
- 失败原因分布（`premature_stop` / `too_few_tool_calls` / `missing_root_cause` 等）
- token 消耗对比（on 组可能因多读 handbook 而略高，需关注增量是否换回命中率提升）

> 位置级 Recall/Precision/F1 + 防泄漏 judge（设计 §13.2 完整指标）留待阶段 2，需用户提供带文件/符号标注的样本。

## 验收标准

1. `SATH_HANDBOOK_ENABLED=false` 启动 portal 后，agent 对话中**不出现** code-map/handbook skill 目录（可观察：off 组 agent 无 `skill_view` handbook 的路径，只有 `rca_*` 工具）。
2. 默认（不设环境变量）行为与现状完全一致（开关默认 true，不破坏现有 agent）。
3. `evals/tasks/rca_handbook.jsonl` 有 ≥3 道真实故障题，每道有可判定的 `root_cause`。
4. off/on 两次跑分产出可对比的命中率与 token 数据。

## 待用户提供

- 3~5 道真实故障描述 + 对应根因关键词（`root_cause`），以及一个可生成 handbook 的真实业务仓库。
