# S19 收口：发货 yaml Growth 复盘开关默认关闭

**日期**: 2026-09-05  
**状态**: 已确认（S18 leftover；P4 默认路径；2026-09-05 实施）  
**范围**: `portal/configs/config.yaml` 与 `config.docker.yaml` 的 Growth 布尔开关。不改 proto 默认、不改 worker Loop、不删 Growth 包。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S18](./2026-09-05-nudge-default-off-design.md)（nudge 代码默认已 false）

**一句话**: 代码默认已经关 Growth，发货 yaml 不得再把 LLM 复盘 / C2s / learnings 打开。

---

## 1. 背景

S12–S18 把默认 Chat 从 Growth 钩子拆开，nudge 代码默认 false，worker 仅 `worker_enabled` 时构造。但发货配置仍写着：

```text
llm_review_enabled: true
session_end_skill_review_enabled: true
learnings_review_enabled: true
```

`worker_enabled` 现网是 false，所以这三项现在不跑。一旦有人只打开 worker、不改其余开关，就会带着 LLM 复盘与 learnings 注入上线。C2s 标志会写进 `GrowthUsecase`；若以后误接回 Chat 会话结束钩子，也会默认点火。P4：Growth 不进默认路径。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `llm_review_enabled` | 发货 yaml **false**（opt-in 仍可改 yaml / `SATH_GROWTH_LLM_REVIEW_ENABLED` 在无 growth 节时） |
| `session_end_skill_review_enabled` | 发货 yaml **false**（与 proto 注释默认一致） |
| `learnings_review_enabled` | 发货 yaml **false** |
| 已是 false 的 `worker_enabled` / `curator_enabled` / `session_end_memory_review_enabled` / `combined_review_enabled` | **保持 false** |
| proto `bool` 缺省 | **不改**（本来就是 false；洞在 yaml 覆盖） |
| worker / curator / assembler | **不改** |

---

## 3. 行为

```text
portal/configs/config.yaml 与 config.docker.yaml：
  llm_review_enabled: false
  session_end_skill_review_enabled: false
  learnings_review_enabled: false
  worker_enabled: false          # 已是
  curator_enabled: false         # 已是
```

打开 worker 时必须显式再打开想要的复盘开关。

---

## 4. 非目标

- 不删 `framework/growth`、不改 GrowthWorker Loop
- 不改 `skills.auto_route_enabled`（P3 已拆 SKILL 预注入；该键现网无调用者）
- 不改 Channel `auto_route_*`（Gateway 路由，不是 SKILL 正文）
- 不改 hypertool（配置默认已 false）
- 不合 assembler

---

## 5. 成功标准

1. 发货 `config.yaml` / `config.docker.yaml` 上述三项为 false。
2. 单测锁定这两份文件的 Growth 默认关开关。
3. `cd portal && go test ./internal/conf -count=1` 绿。
