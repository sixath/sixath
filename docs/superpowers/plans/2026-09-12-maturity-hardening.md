# 功能成熟度补齐 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Sixath 从「能力面接近生产、运营面落后」的 L2.5 状态推进到 L3（可内部生产）：外部抖动不致命、上下文与成本可预测、改动有自动化门禁与量化回归、全链路可观测、长会话可连续、取消语义明确。

**Architecture:** 不改动已验证的 ReAct 主路径语义，全部改动以「装饰器 / 可插拔接口 / 可选模式」形态叠加：模型韧性放在 `factory` 出口装饰，token 精算抽 `TokenCounter` 接口并保留 rune 粗估兜底，工具治理收敛到 Registry 前置校验 + 统一超时 + `ApprovalPolicy`，Gateway 补 metrics/结构化日志/幂等接口化，规划能力作为 `Agent.Mode` 可选项与 ReAct 并存。

**Tech Stack:** Go 1.26（`framework/`、`portal/`、`gateway/` 三个 module，portal 通过 `replace ../framework` 引用本地）、React 19 + Vite 8 + Playwright（`web/`）、MySQL 8 / GORM、Docker Compose、GitHub Actions。

**Repos:** monorepo 根为编排仓；`portal/`、`web/`、`gateway/` 为目录内模块。**Do not commit unless asked.**

**非目标：**
- 不重写 ReAct 主循环，不做框架级重构。
- 不引入新的模型 provider（仅补既有 openai/dashscope/ollama 的韧性）。
- 不做 UI 视觉改版（仅补历史分页、取消态、plan 面板等新交互）。
- 不在本计划内执行 Git 历史重写（属破坏性操作，需单独确认）。

---

## 执行状态快照（截至 2026-09-12）

> 19 个 Task 中核心已全部落地；剩余为跨服务集成收尾与需部署形态确认的项，详见各 Task 段落。

| Task | 状态 | 备注 |
|------|------|------|
| 1 CI 门禁 | 完成 | `.github/workflows/ci.yml`（含 evals job） |
| 2 凭据出库 | 完成 | `${VAR}` 插值 + example + .gitignore |
| 3 模型重试 | 完成 | `resilient.go` 装饰器 + 测试 |
| 4 token 计数 | 完成 | token 校准 + `estimated_cost` 成本估算（默认计价表，可覆盖） |
| 5 参数校验 | 完成 | Registry 前置 JSON Schema |
| 6 超时+并行 | 完成 | 统一超时 + 并行默认开 |
| 7 ApprovalPolicy | 完成 | 审批收敛 |
| 8 Gateway 可观测 | 完成 | `/metrics` + slog + traceparent |
| 9 幂等接口+租约 | 完成 | 接口化完成；单副本已确认（ADR-0002）；`Leader` 接口 + `AlwaysLeader` 已预留（多副本可 drop-in 分布式租约） |
| 10 投递重试+记录 | 完成 | `DeliverWeCom` + `channel_deliveries` |
| 11 游标分页 | 完成 | 会话历史分页 |
| 12 取消语义 | 完成 | 显式取消 + 部分结果保留 |
| 13 eval 体系 | 完成 | 数据集 + runner + 门禁 + 工具名对齐 + mock 驱动（CI 回归）+ live 模式（真实模型） |
| 14 Plan-Execute | 完成 | framework 核心 + `BuildAgent` 选择器 + `Mode` 走 DB（含 proto API）+ SSE plan 事件流 + Web 面板（PlanPanel） |
| 15 Skill 热重载 | 完成 | framework `Watcher` 完成；portal 每 turn 重扫 skills（`BuildSkillsIndex`），已天然热加载，无需 reload 路由 |
| 16 多渠道出站 | 完成 | 抽象 + cron 迁移 + `send_to_wecom` 泛化（按绑定选 wecom/wxpusher，重试+记录统一走 `Deliver`） |
| 17 Growth 收口 | 完成 | flag 决策 + TODO 收口 |
| 18 KPI 面板 | 完成 | `docs/maturity-kpis.md` |
| 19 文档/残留 | 完成 | README 重写 + ADR + Runbook + 清残留 |

**全部 19 个 Task 均已落地**（含 Task 13 live 模式，真实模型跑批回填 baseline）。

---

## 成熟度门槛（验收靶子）

| # | 门槛 | 当前证据 | 归属 Task |
|---|------|----------|-----------|
| G1 | 单次外部抖动不导致任务失败 | `framework/model/` 全目录无 retry/backoff；`framework/model/openai.go` 失败直接返回 | Task 3 |
| G2 | 上下文 / 成本可精确预测 | `framework/model/estimate_tokens.go:9` `alpha=1.35` 码点粗估；`context_pipeline.go:65` 用 alpha 反推 rune 预算 | Task 4 |
| G3 | 每个 PR 有自动化质量门禁 | 无 `.github/`；仅 `_neo4j_q/*.ps1` + `portal/Makefile` | Task 1 |
| G4 | 仓库无明文凭据 | `gateway/configs/channels.yaml` 含企微 `bot_id`/`secret`，与根 `README.md:96` 声明矛盾 | Task 2 |
| G5 | 工具参数在进入 Execute 前被校验 | `framework/tool/tool.go:25` `Parameters` 仅 map，无 schema 校验器 | Task 5 |
| G6 | 全链路可观测（含 Gateway） | `gateway/cmd/gateway/main.go` 仅 `log` + `/healthz:60`；无 metrics/tracing | Task 8 |
| G7 | 长会话历史可连续回放 | `portal/internal/data/chat_mysql.go:292` `ListBySession` limit<=0 → 100，无游标 | Task 11 |
| G8 | 改动效果可量化 | `framework/` 223 单测、仅 3 个 bench、无 eval 集；skill harness 仅存在于 `skills_examples/` | Task 13 |
| G9 | 取消 / 中断语义明确 | 仅 `ctx.Done()` 在 channel send 处 select；无 Cancel API、无部分结果保留 | Task 12 |
| G10 | 多步任务有结构化规划 | `framework/agent/plan_agent.go` 仅把自然语言塞 `Metadata["plan"]` 后透传 | Task 14 |

---

## 排期总览

| 周 | 阶段 | Tasks | 阶段出口 |
|----|------|-------|----------|
| 1 | P0 可信运行 | Task 1 → Task 2 | CI 门禁生效、凭据出库 |
| 2 | P0 可信运行 | Task 3 → Task 4 | G1/G2/G3/G4 达成 |
| 3–4 | P1 工程化 | Task 5 → Task 8 | 工具治理 + Gateway 可见 |
| 5–6 | P1 工程化 | Task 9 → Task 12 | G5/G6/G7/G9 达成 |
| 7–9 | P2 能力补强 | Task 13（优先） | G8 达成 + baseline 报告 |
| 10–12 | P2 能力补强 | Task 14 → Task 17 | G10 达成 |
| 13+ | P3 持续度量 | Task 18 → Task 19 | KPI 面板 + 文档对齐 |

**依赖链：** Task 1 →（所有 Task）；Task 4 → Task 8（成本指标）、Task 13（成本口径）；Task 5 → Task 14（步骤判据）；Task 11 与 Task 9/10 可并行。
**并行建议：** P0 阶段 2 人（framework+portal / 平台+CI）；P1 起 3 人（framework / portal+gateway / web+eval）。

---

## 现状锚点

| 位置 | 现状行为 |
|------|----------|
| `framework/model/factory.go:21` `NewModelFromConfig` | provider switch：openai / dashscope / ollama，无包装层 |
| `framework/model/factory.go:78` `NewFromIdentifier` | 插件 `RegisterProvider` 优先，后回退内置 |
| `framework/model/estimate_tokens.go:13` | `EstimateTokensConservative` = Σ rune × alpha，alpha 默认 1.35 |
| `framework/model/context_budget.go:11` | `DefaultMaxContextRunes = 200_000`（码点预算） |
| `framework/model/context_pipeline.go:19` | 管线 L1 → snip → L0 预剪枝 → L0 → strip 孤儿 tool → L2 摘要 |
| `framework/model/model.go:111–118` | `TokenEstimateAlpha` / `ContextTrace` / `L2` / `SnipCompactEnabled` 选项已存在 |
| `framework/agent/react_agent.go:260` | `MaxSteps` 默认 3（Skill handler 内为 20） |
| `framework/agent/react_agent.go:55` | `ParallelTools` 默认 false |
| `framework/agent/react_agent.go:907` | 达 MaxSteps 走 `forceFinalSummary` 强制收尾 |
| `framework/tool/tool.go:25` | `Tool{Name,Description,Parameters,Execute,Toolset,CheckFn,RequiresSequential,…}` |
| `framework/tool/` | 延迟加载（`defer.go`）、目录检索（`catalog_search.go` BM25）、审批散落 `*_pending.go` |
| `framework/skills/index.go:22` | `NewIndex` 启动期一次 `WalkDir` 扫描，无 watch |
| `framework/memorysearch/builtin.go` | 已有 fsnotify 监听（可复用的既有模式） |
| `portal/internal/data/chat_mysql.go:292` | `ListBySession(sessionID, limit)`；limit<=0 → 100 |
| `portal/internal/chat/compact_boundary.go:25` | 幂等扫描用 `compactBoundaryListLimit = 10000` |
| `portal/internal/server/http.go:176` | `/metrics` 已暴露（portal 侧有） |
| `portal/internal/service/growth_agent_review.go:124` | `// TODO(phase-2): full-tools 通用工具集入口待接入。` |
| `gateway/cmd/gateway/main.go:44–45` | session router 30s 缓存；幂等 10 分钟内存 TTL |
| `gateway/cmd/gateway/main.go:60` | 唯一探针 `GET /healthz` |
| `gateway/configs/channels.yaml` | 已提交真实形态 `bot_id` / `secret` |
| `portal/openapi.yaml`、根 `main.go`/`go.mod` | protoc / GoLand 模板残留（`Greeter`、`module sixath`） |
| `.github/` | 不存在 |

---

## File map

| Path | Responsibility | Phase |
|------|----------------|-------|
| `.github/workflows/ci.yml` | Create：framework/portal/gateway/web 四 job 门禁 | P0 |
| `.github/workflows/nightly.yml` | Create：live eval + 长跑冒烟 | P2 |
| `gateway/configs/channels.example.yaml` | Create：占位符版本 | P0 |
| `gateway/configs/channels.yaml` | Modify：移出仓库 + `.gitignore` | P0 |
| `gateway/internal/config/config.go` | Modify：env 插值 `${VAR}` | P0 |
| `framework/model/resilient.go` | Create：重试/退避装饰器（Model/Streaming/ToolCalling） | P0 |
| `framework/model/resilient_test.go` | Create：429/5xx/超时/首 token 后失败 | P0 |
| `framework/model/factory.go` | Modify：出口统一包装饰器 | P0 |
| `framework/model/token_counter.go` | Create：`TokenCounter` 接口 + 校准实现 | P0 |
| `framework/model/estimate_tokens.go` | Modify：保留粗估为 fallback，新增 effective alpha | P0 |
| `framework/model/context_pipeline.go` / `context_budget.go` / `l2_runtime.go` | Modify：触发点切 token 预算 | P0 |
| `framework/model/openai.go` | Modify：usage 回填校准 + 成本字段 | P0 |
| `framework/tool/validate.go` | Create：JSON Schema 校验 + 结构化错误 | P1 |
| `framework/tool/tool.go` | Modify：`Timeout` / `RiskLevel` 字段；Registry 前置校验 | P1 |
| `framework/tool/approval.go` | Create：统一 `ApprovalPolicy` 引擎 | P1 |
| `framework/agent/react_agent.go` | Modify：统一超时注入、并行默认开、Cancel API | P1 |
| `gateway/internal/observability/` | Create：metrics + slog + traceparent 注入 | P1 |
| `gateway/cmd/gateway/main.go` | Modify：挂 `/metrics`、中间件、优雅关闭指标 | P1 |
| `gateway/internal/idempotency/store.go` | Modify：抽接口 + 内存/Redis 实现 | P1 |
| `gateway/internal/wecom/lease.go` | Create：wecom_bot 单副本租约 | P1 |
| `portal/internal/channel/wecom.go` | Modify：退避重试 + 投递记录 | P1 |
| `portal/internal/data/model/channel_delivery.go` | Create：`channel_deliveries` 表 | P1 |
| `portal/internal/data/chat_mysql.go` | Modify：游标分页 `(created_at,id)` | P1 |
| `portal/internal/runtime/http.go` / `service.go` | Modify：messages `before`/`limit` | P1 |
| `portal/internal/service/chat_stream.go` | Modify：`cancelled` / `plan` / `plan_step` 事件 | P1/P2 |
| `web/src/pages/ChatPage.tsx` / `components/SessionSidebar.tsx` | Modify：向上加载历史、中断态、plan 面板 | P1/P2 |
| `evals/`（顶层） | Create：任务集 + runner（mock/live） + 报告 | P2 |
| `framework/agent/plan_agent.go` | Rewrite：结构化 Plan + verify + replan | P2 |
| `framework/skills/index.go` | Modify：fsnotify 增量索引 | P2 |
| `portal/internal/server/skill_http.go`（或现有 skills 路由） | Modify：`POST /api/v1/skills/reload` | P2 |
| `docs/superpowers/specs/2026-09-12-maturity-hardening-design.md` | Optional：本计划的权威规格（按需补） | P0 |

---

# P0：可信运行

### Task 1: CI 门禁（G3）

**Files:**
- Create: `.github/workflows/ci.yml`
- Modify: `portal/Makefile`（可选：抽出 `ci` target 供本地复用）

**说明：** 先建门禁，后续所有改动才有保护。四个 job 相互独立，允许并行失败。

- [ ] **Step 1: 确认三条 go module 均可独立构建**

```bash
cd framework && go build ./... && go test ./... -count=1
cd portal && go build ./... && go test ./... -count=1
cd gateway && go build ./... && go test ./... -count=1
```

Expected: 全部通过。若 portal 测试依赖 MySQL，记录需要 `services:` 的包清单，后续 job 内起 `mysql:8.0` 服务容器。

- [ ] **Step 2: 写 `ci.yml` 骨架**

```yaml
name: ci
on:
  pull_request:
  push:
    branches: [main]
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
jobs:
  framework:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: framework } }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: framework/go.mod, cache-dependency-path: framework/go.sum }
      - run: go build ./...
      - run: go test ./... -race -count=1
  portal:    # working-directory: portal，需 services: mysql:8.0 + init
  gateway:   # working-directory: gateway
  web:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: web } }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: 20, cache: npm, cache-dependency-path: web/package-lock.json }
      - run: npm ci
      - run: npm run test
      - run: npm run build
      - run: npx playwright install --with-deps chromium
      - run: npm run test:e2e      # mock 模式，不依赖后端
```

- [ ] **Step 3: 追加 lint 与配置校验 job**

```yaml
  lint:
    steps:
      - uses: golangci/golangci-lint-action@v6   # framework / portal / gateway 各一次
      - run: docker compose config -q
      - uses: gitleaks/gitleaks-action@v2        # 见 Task 2
```

- [ ] **Step 4: 设为必需检查**

仓库 Settings → Branches → 保护 `main`：勾选全部 job 为 required status checks。

- [ ] **Step 5: 验证**

开一个只改注释的 PR，确认四条 job 全绿且耗时在可接受范围（目标 < 8 分钟，必要时拆分 cache key）。

---

### Task 2: 凭据出库（G4）

**Files:**
- Create: `gateway/configs/channels.example.yaml`
- Modify: `gateway/configs/channels.yaml`（不再入库）、`.gitignore`、`gateway/internal/config/config.go`、`docker-compose.yml`、`gateway/README.md`

- [ ] **Step 1: 生成 example，脱敏真实文件**

`channels.example.yaml` 用占位符；本地 `channels.yaml` 改为引用环境变量：

```yaml
channels:
  - id: wecom-demo
    type: wecom_bot
    enabled: true
    bot_id: ${WECOM_BOT_ID}
    secret: ${WECOM_BOT_SECRET}
    # 可选：corp_id / corp_secret 走成员姓名解析
```

- [ ] **Step 2: `config.go` 支持 `${VAR}` 插值**

在 `channel.Load` 读取 YAML 之后、反序列化之前对原始文本做 `os.ExpandEnv`（或仅匹配 `${...}` 的正则替换，避免误伤 `$` 字面量）。缺失变量时报错并指出变量名，**不静默变空串**。

- [ ] **Step 3: 测试**

```bash
cd gateway && go test ./internal/config/... ./internal/channel/... -count=1
```

新增用例：`${VAR}` 已设置 → 替换成功；未设置 → 返回含变量名的错误；`$$` / 裸 `$` 字面量不被破坏。

- [ ] **Step 4: 出库与文档**

- `git rm --cached gateway/configs/channels.yaml`，加入 `.gitignore`
- `docker-compose.yml` gateway 段补 `WECOM_BOT_ID` / `WECOM_BOT_SECRET` 环境变量透传
- `gateway/README.md` 与根 `README.md:96` 的说明改为「复制 example 并设置环境变量」

- [ ] **Step 5: 凭证轮换（人工）**

企微控制台重新生成 BotID/Secret 并写入本地 `.env`。**必做**——历史提交里的凭证视为已泄露。

- [ ] **Step 6: 历史清理（需单独确认，默认不做）**

`git filter-repo --path gateway/configs/channels.yaml --invert-paths`。属破坏性操作，执行前需用户明确同意，并通知所有协作者重新克隆。

---

### Task 3: 模型调用韧性（G1）

**Files:**
- Create: `framework/model/resilient.go`、`framework/model/resilient_test.go`
- Modify: `framework/model/factory.go`、`framework/model/model.go`（配置项）、`portal/internal/chat/agent_builder.go`（透传配置）

- [ ] **Step 1: 写失败测试**

```go
func TestResilientModel_RetriesOn429ThenSucceeds(t *testing.T) {
	// fake Model: 第 1、2 次返回 &APIError{Status:429, RetryAfter:0}，第 3 次返回正常响应
	// 断言：最终 err == nil，调用次数 == 3
}
func TestResilientModel_DoesNotRetryOn400(t *testing.T) { /* 调用次数 == 1 */ }
func TestResilientModel_StreamRetryOnlyBeforeFirstToken(t *testing.T) {
	// 首 token 前失败 → 重试；首 token 后失败 → 立即返回错误，调用次数 == 1
}
func TestResilientModel_ParentContextCancelStopsRetry(t *testing.T) { /* 立即返回 ctx.Err() */ }
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework && go test ./model/ -run TestResilientModel -count=1
```

Expected: `undefined: resilientModel`。

- [ ] **Step 3: 实现装饰器**

```go
type RetryConfig struct {
	MaxAttempts int           // 默认 3
	BaseDelay   time.Duration // 默认 500ms
	MaxDelay    time.Duration // 默认 8s
	Jitter      bool          // 默认 true（full jitter）
}

type resilientModel struct {
	inner Model
	cfg   RetryConfig
}

// 同时实现 StreamingModel / ToolCallingStreamingModel（按 inner 类型断言并按需包装）
func WrapResilient(m Model, cfg RetryConfig) Model
```

实现要点：
- `retryable(err)` 判定：HTTP 429、5xx、`net.Error`、`context.DeadlineExceeded`（且父 ctx 未取消）、provider 侧超时；**排除** 400/401/403/404 与业务错误。
- 退避：`delay = min(MaxDelay, BaseDelay * 2^attempt)`，jitter 时取 `rand(delay)`；若响应带 `Retry-After` 则优先使用（设上限）。
- 流式：包装 `ChatStream` / `ChatWithToolsStream`，仅当**尚未产生任何 delta** 时允许重试；一旦收到内容则不再重试，直接返回错误。
- 打点：把 `attempts`、`last_error_class` 写入返回的 `RunTrace` 相关字段或 OTel span attribute。

- [ ] **Step 4: 配置与装配**

- `model.ModelConfig` 增加 `Retry RetryConfig`；`portal/configs/*.yaml` 增 `model.retry.*`，默认 `max_attempts: 3`。
- `factory.go:21` 与 `factory.go:78` 出口统一 `return WrapResilient(m, cfg.Retry), nil`。
- **注意**：`RegisterProvider` 的外部插件返回值同样经过包装，确保行为一致。

- [ ] **Step 5: 通过 + 回归**

```bash
cd framework && go test ./model/... -race -count=1
cd portal && go test ./internal/chat/... -count=1
```

- [ ] **Step 6: Commit**

`feat(model): add retry/backoff decorator for provider calls`

---

### Task 4: 精确 token 与成本核算（G2）

**Files:**
- Create: `framework/model/token_counter.go`、`framework/model/token_counter_test.go`
- Modify: `framework/model/estimate_tokens.go`、`context_pipeline.go`、`context_budget.go`、`l2_runtime.go`、`openai.go`
- Modify: `portal/internal/service/chat.go`（成本写入 `turn_trace`）

- [ ] **Step 1: 写失败测试**

```go
func TestEffectiveAlpha_TracksActualUsage(t *testing.T) {
	// 注入 20 次 estimated=130 / actual=100 → alphaEffective 收敛到 ≈1.30（±0.05）
}
func TestPrepareChatContext_UsesTokenBudget(t *testing.T) {
	// MaxContextTokensSoft=1000，构造 2000 token 的消息 → 触发 L0 裁剪，且裁剪后 EstimateTokens <= 1000
}
func TestEstimateTokens_FallbackWhenNoCalibration(t *testing.T) { /* 无样本时行为与今日一致 */ }
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework && go test ./model/ -run 'TestEffectiveAlpha|TestPrepareChatContext_UsesTokenBudget' -count=1
```

- [ ] **Step 3: 定义 `TokenCounter` 接口与三种实现**

```go
type TokenCounter interface {
	Count(msgs []Message) int
	Alpha() float64 // 供预算换算与可观测
}

// 1) ConservativeCounter：保留 EstimateTokensConservative（fallback）
// 2) CalibratedCounter：滑动窗口维护 estimated/actual 比值，alpha_effective = clamp(ema, 0.8, 2.0)
// 3) TiktokenCounter（可选）：OpenAI 系精确计数
```

`CalibratedCounter.Observe(estimated, actual int)` 由 provider 层在拿到 `resp.Usage` 时调用；按 `provider/model` 维度隔离。

- [ ] **Step 4: 切换触发点到 token 预算**

- `context_pipeline.go:65–74`：删除 `budgetRunes = tokens/alpha` 的反推，直接传 token 预算给裁剪器；`CompressMessagesByRunesBudget` 保留为兜底路径（rename 为 `CompressMessagesByBudget` 并接受 `TokenCounter` 更佳，保持旧函数为 wrapper 避免破坏调用方）。
- `l2_runtime.go`：软阈值判定改用 `counter.Count(msgs)`。
- `context_budget.go:11` 的 `DefaultMaxContextRunes` 保留但标记为「fallback 用」；新增 `DefaultMaxContextTokensSoft`。

- [ ] **Step 5: 成本与用量落库**

- `openai.go` 取 `resp.Usage`：调用 `counter.Observe(estimated, prompt+completion)`，并把 `prompt_tokens/completion_tokens/estimated_cost` 放入 `RunTrace`。
- `portal/internal/service/chat.go` 写入 `turn_trace`；SSE `model_call` payload 增加同字段。

- [ ] **Step 6: 通过 + 校验误差**

```bash
cd framework && go test ./model/... -count=1
cd portal && go test ./internal/service/... -count=1
```

回放一段真实会话的 usage 记录，确认估算误差中位数 < 15%（把该回放写成 `token_counter_test.go` 的表驱动用例，作为长期回归）。

- [ ] **Step 7: Commit**

`feat(model): token-counting abstraction with usage calibration`

---

# P1：工程化

### Task 5: 工具参数统一校验（G5）

**Files:**
- Create: `framework/tool/validate.go`、`framework/tool/validate_test.go`
- Modify: `framework/tool/tool.go`（Registry 前置校验）、引入 `github.com/santhosh-tekuri/jsonschema/v6`

- [ ] **Step 1: 写失败测试**

```go
func TestRegistryExecute_RejectsMissingRequired(t *testing.T) {
	// 工具 required=["path"]，传 {} → 返回 *InvalidArgumentsError，Execute 未被调用
}
func TestRegistryExecute_RejectsWrongType(t *testing.T) { /* path: 123 → 错误含字段路径 /path */ }
func TestRegistryExecute_ErrorIsModelActionable(t *testing.T) {
	// 错误文本可被模型理解并修正：含 missing/invalid + 字段名 + 期望类型
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework && go test ./tool/ -run TestRegistryExecute_ -count=1
```

- [ ] **Step 3: 实现校验层**

```go
type InvalidArgumentsError struct {
	Tool   string
	Errors []SchemaError // {Path, Keyword, Message}
}
func (e *InvalidArgumentsError) Error() string // 单项直出；多项摘要 + 最多列 5 条
```

要点：
- 编译缓存：按工具名缓存编译后的 schema（Registry 注册时预编译，失败则 `Register` 返回错误）。
- 宽松兼容：`Parameters` 为空或不合法时**跳过校验**并记 warn（避免一次性打破 30+ 工具的既有行为）。
- 错误处理与现有循环一致：返回 `nil` error，把错误作为工具结果交回模型重试（与 `react_agent.go` 现有「参数 JSON 损坏不中断循环」的策略统一）。

- [ ] **Step 4: 全量工具体检**

新增 `tool/validate_coverage_test.go`：遍历 Registry，输出「已声明 schema 的工具数 / 总数 / schema 编译失败清单」，作为逐步补全的基线（不为 0 才允许合并，作为进度门槛）。

- [ ] **Step 5: 通过 + Commit**

```bash
cd framework && go test ./tool/... -count=1
```

`feat(tool): validate arguments against declared JSON schema`

---

### Task 6: 统一超时 + 并行默认开启

**Files:**
- Modify: `framework/tool/tool.go`、`framework/agent/react_agent.go`、`framework/agent/react_parallel_tools_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestToolTimeout_AppliesFromConfig(t *testing.T) {
	// 工具 sleep 200ms，超时配置 50ms → 结果含 deadline exceeded，循环继续
}
func TestParallelTools_DefaultEnabled(t *testing.T) {
	// 默认配置下两个可并行工具的总耗时 < 串行之和 * 0.7
}
func TestRequiresSequential_FallsBackToSerial(t *testing.T) { /* 标记工具不被并行调度 */ }
```

- [ ] **Step 2: 加字段与注入点**

- `Tool.Timeout time.Duration`（0 → 取配置默认，默认 60s）。
- `react_agent.go` 执行工具前统一 `context.WithTimeout(ctx, effectiveTimeout)`，替换/包裹各工具内部已有超时（保留 SSH、script、MCP 的更细粒度设置，作为更短的上限）。
- 移除散落超时的重复默认值，统一到 `tool.DefaultTimeout`。

- [ ] **Step 3: 并行默认开启**

- `react_agent.go:55` `ParallelTools` 默认改 true，新增 `MaxParallelTools`（默认 8，沿用 `:604` 现值）。
- **前置审计**：逐个检查工具的 `RequiresSequential` 是否正确（写入/审批/终端/浏览器类必须串行）；审计结论落在代码注释与测试用例中。
- 保留 kill switch：`SATH_PARALLEL_TOOLS=0` 或配置 `parallel_tools: false`。

- [ ] **Step 4: 通过 + race + Commit**

```bash
cd framework && go test ./agent/... ./tool/... -race -count=1
```

`feat(agent): unified tool timeout and parallel tools by default`

---

### Task 7: 审批策略收敛为 `ApprovalPolicy`

**Files:**
- Create: `framework/tool/approval.go`、`framework/tool/approval_test.go`
- Modify: `framework/tool/file_pending.go`、`terminal_pending.go`、`browser_pending.go`、`tool/skillops/skill_manage_pending.go`

- [ ] **Step 1: 定义风险级别与策略**

```go
type RiskLevel string // read | write | destructive | network

type ApprovalPolicy struct {
	AlwaysAllow []string             // 工具名白名单
	RequireFor  map[RiskLevel]bool   // 按级别要求确认
	SessionTTL  time.Duration        // 会话级授权缓存（默认 10m）
}
func (p *ApprovalPolicy) Decide(toolName string, risk RiskLevel, sessionID string) Decision
```

- [ ] **Step 2: 等价性测试（迁移的核心保障）**

为现有四个 pending 场景各写一个用例，断言「策略化后行为与今日一致」：
- workspace 文件写/删除 → 需确认
- 终端危险命令 → 需确认
- 浏览器提交类动作 → 需确认
- `skill_manage` 写操作 → 需确认，且保留 `WithSkillManageUIConfirm` 特权路径

- [ ] **Step 3: 迁移并保留返回契约**

四个 `*_pending.go` 改为调用 `ApprovalPolicy.Decide`，但**保持 `tool/confirm_error.go` 的 expired/not_found 结构化错误与 token 形态不变**（Portal/Web 确认卡依赖它）。

- [ ] **Step 4: 通过 + Commit**

```bash
cd framework && go test ./tool/... -count=1
cd portal && go test ./internal/chat/... ./internal/service/... -count=1
cd web && npm run test
```

`refactor(tool): converge pending-confirm logic into ApprovalPolicy`

---

### Task 8: Gateway 可观测性与 trace 透传（G6）

**Files:**
- Create: `gateway/internal/observability/metrics.go`、`logging.go`、`middleware.go`
- Modify: `gateway/cmd/gateway/main.go`、`gateway/internal/runtimeclient/client.go`、`gateway/go.mod`

- [ ] **Step 1: metrics**

暴露 `GET /metrics`（Prometheus，复用 Portal 的依赖版本）：

| 指标 | 类型 | 标签 |
|------|------|------|
| `gateway_requests_total` | counter | channel, path, status |
| `gateway_request_duration_seconds` | histogram | channel, path |
| `gateway_idempotency_hits_total` | counter | channel, result(in_progress/done) |
| `gateway_portal_errors_total` | counter | method, status_class |
| `gateway_wecom_active_connections` | gauge | — |
| `gateway_wecom_reconnects_total` | counter | — |

- [ ] **Step 2: 结构化日志**

`log/slog` JSON handler；中间件为每请求生成 `request_id` 并注入 context；关键路径（webhook 收单、幂等命中、portal 调用失败、wecom 断连）结构化字段化，替代 `log.Printf`。

- [ ] **Step 3: traceparent 透传**

中间件从入站请求提取 `traceparent`（无则新建），调用 Portal 时放入 header；Portal 侧已有 OTel 中间件，可直接续接链路。验证：Gateway → Portal → framework 的 span 在同一 trace 下。

- [ ] **Step 4: 测试 + Compose 验证**

```bash
cd gateway && go test ./... -race -count=1
docker compose up --build -d && curl -s localhost:18088/metrics | head -50
```

- [ ] **Step 5: Commit**

`feat(gateway): metrics, structured logging and trace propagation`

---

### Task 9: 幂等存储接口化 + wecom 单副本租约

**Files:**
- Modify: `gateway/internal/idempotency/store.go`、`gateway/cmd/gateway/main.go`
- Create: `gateway/internal/idempotency/redis_store.go`、`gateway/internal/wecom/lease.go`

- [ ] **Step 1: 抽接口**

```go
type Store interface {
	Begin(ctx context.Context, key string) (state State, reused bool, err error)
	Mark(ctx context.Context, key string, state State) error
}
// memory.Store 保持今日语义（10m TTL）；redis.Store 用 SETNX + EXPIRE，支持多副本
```

测试：同一个 key 并发 `Begin` → 仅一个 `reused=false`（对内存与 Redis 实现各跑一遍，Redis 用 `miniredis`）。

- [ ] **Step 2: wecom_bot 租约**

`lease.go`：向 Portal 申请/续约 workspace 租约（复用 `growth_workspace_leases` 的成熟模式，或新增 `gateway_bot_leases`）；未持租约的副本**不订阅** WSS 并打 warn 指标；租约丢失时主动断开并在退避后重新竞争。

- [ ] **Step 3: 验证**

- 双副本启动同一 `bot_id`：仅一个建立连接，另一个日志明确「未持租约」。
- 杀掉持租约副本：另一副本在租约 TTL（默认 30s）内接管。

- [ ] **Step 4: Commit**

`feat(gateway): pluggable idempotency store and bot single-instance lease`

---

### Task 10: 出站投递重试与投递记录

**Files:**
- Create: `portal/internal/data/model/channel_delivery.go`、`portal/internal/service/delivery_worker.go`（或并入现有 channel service）
- Modify: `portal/internal/channel/wecom.go`、`portal/internal/data/data.go`（AutoMigrate）、`internal/chat/wecom_wiring.go`

- [ ] **Step 1: 表结构**

```go
type ChannelDelivery struct {
	ID          string    `gorm:"primaryKey;size:36"`
	ChannelID   string    `gorm:"size:64;index"`
	SessionID   string    `gorm:"size:36;index"`
	Payload     string    `gorm:"type:text"`
	Status      string    `gorm:"size:16;index"` // pending|sent|failed
	Attempts    int
	LastError   string    `gorm:"type:text"`
	NextRetryAt *time.Time `gorm:"index"`
	CreatedAt, UpdatedAt time.Time
}
```

- [ ] **Step 2: 写失败测试**

```go
func TestWeComDeliver_RetriesOn5xxThenSucceeds(t *testing.T)  // 3 次内成功 → status=sent, attempts>=2
func TestWeComDeliver_MarksFailedAfterMaxAttempts(t *testing.T)
func TestWeComDeliver_DoesNotRetryOnInvalidWebhook(t *testing.T) // 4xx/errcode → 立即 failed
func TestDeliverTruncatesUTF8Safely(t *testing.T)              // 4096 字节边界不切坏多字节字符
```

- [ ] **Step 3: 实现**

- `PushToWeCom` 保持纯发送语义，重试与记录放到 worker/包装层。
- 指数退避 + 上限（默认 5 次、最长 10 分钟）；`errcode != 0` 与 4xx 视为不可重试。
- `send_to_wecom` 工具的成功语义改为「已入队」（返回 delivery id），避免模型把「已入队」当「已送达」。**注意这会改变工具返回文案，需同步 `web` 的文案映射与既有测试。**
- `wecom_wiring.go` 的进程内 1s/session 限流抽为可配置；多副本下可选 Redis 实现。

- [ ] **Step 4: 通过 + Commit**

```bash
cd portal && go test ./internal/channel/... ./internal/service/... -count=1
```

`feat(channel): outbound retry with delivery records`

---

### Task 11: 消息游标分页（G7）

**Files:**
- Modify: `portal/internal/data/chat_mysql.go`、`portal/internal/biz/*`（Repo 接口）、`portal/internal/runtime/http.go`、`portal/internal/runtime/service.go`、`portal/internal/service/chat.go`
- Modify: `web/src/pages/ChatPage.tsx`、`web/src/components/SessionSidebar.tsx`、`web/src/api/client.ts`
- Modify: `web/e2e/session-history.spec.ts`（或新增分页 spec）

- [ ] **Step 1: 语义测试先行（关键风险点）**

```go
func TestListBySessionCursor_ExcludesInactive(t *testing.T)         // rewind 后 active=false 不返回
func TestListBySessionCursor_StableAcrossSameTimestamp(t *testing.T) // (created_at,id) 复合游标不跳不重
func TestListBySessionCursor_BackwardCompatible(t *testing.T)        // 旧签名 limit<=0 → 仍返回最近 100
func TestMessagesAPI_DefaultLimitAndCap(t *testing.T)                // 默认 50，上限 200
```

必须覆盖与 `SoftDeactivateAfter`（rewind）和 compact boundary（`compact_boundary.go` 写入的 `sixath.origin=compact_boundary` 消息）的组合场景。

- [ ] **Step 2: 仓储层**

`ListBySession` 保持不动（向后兼容）；新增 `ListBySessionBefore(ctx, sessionID, beforeCursor, limit)`，游标为 `created_at + id` 的复合比较，`Order("created_at ASC, id ASC")` 后反转返回。

- [ ] **Step 3: Runtime API**

`GET /runtime/v1/sessions/{id}/messages?before=<cursor>&limit=50`；`before` 缺省 = 最新一页。响应带 `next_cursor`（无更多时为 `""`）。OpenAPI/proto 注释同步更新。

- [ ] **Step 4: Web 交互**

- 进入会话加载最新一页；滚动到顶部触发加载更早（保留滚动锚点，避免跳动）。
- compact boundary 消息在时间线上以分隔条展示（与既有 `compactBoundary` 逻辑一致）。
- 无更多历史时显示明确终点。

- [ ] **Step 5: 验证**

```bash
cd portal && go test ./internal/data/... ./internal/runtime/... -count=1
cd web && npm run test && npm run test:e2e
```

手工：造 1000 条消息会话，验证完整回放且无重复/缺失。

- [ ] **Step 6: Commit**

`feat(chat): cursor pagination for session messages`

---

### Task 12: 取消与中断语义（G9）

**Files:**
- Modify: `framework/agent/react_agent.go`、`framework/agent/agent.go`
- Modify: `portal/internal/service/chat_stream.go`、`portal/internal/service/chat.go`、`portal/internal/runtime/*`
- Modify: `web/src/pages/ChatPage.tsx`、`web/src/api/chatStream.ts`
- Modify: `portal/internal/data/model/chat.go`（消息状态）

- [ ] **Step 1: 失败测试**

```go
func TestReActAgent_Cancel_StopsWithinDeadline(t *testing.T)   // 取消后 3s 内 Run 返回
func TestReActAgent_Cancel_PreservesPartialResult(t *testing.T) // 已产生的 delta 与已完成工具结果可读
func TestReActAgent_Cancel_NoGoroutineLeak(t *testing.T)        // goleak.VerifyNone
func TestChatStream_EmitsCancelledEvent(t *testing.T)
```

- [ ] **Step 2: Agent 层**

- 新增 `Cancel(runID string) error`（`Run` 注册可取消运行表，按 runID 触发 cancel）。
- 事件新增 `cancelled`；取消时保留 `RunTrace` 中已完成步骤与累积 delta。
- 所有内部 goroutine 以 `ctx` 为父，确保 `goleak` 通过。

- [ ] **Step 3: Portal 层**

- `chat_stream.go` 新增 `cancelled` 事件（对齐现有 `error` 的处理方式，但**不**抑制已有内容）。
- 消息落库状态新增 `interrupted`；SSE 断开即触发 Cancel（现有逻辑收敛到该语义）。
- 新增 `POST /runtime/v1/sessions/{id}/turns/{turn_id}/cancel`（或复用现有 turns 路由的取消端点，按现有 API 风格二选一）。

- [ ] **Step 4: Web 层**

- 发送中显示"停止"按钮 → 调用取消 → 时间线标记「已中断」。
- 「继续」按钮：基于已保留的上下文续跑（新 turn，不重放已完成工具）。

- [ ] **Step 5: 验证 + Commit**

```bash
cd framework && go test ./agent/... -race -count=1
cd portal && go test ./internal/service/... ./internal/runtime/... -count=1
```

`feat(agent): explicit cancel semantics with partial result retention`

---

# P2：能力补强

### Task 13: Agent 评测体系（G8，最高优先）

**Files:**
- Create: `evals/README.md`、`evals/tasks/*.jsonl`、`evals/runner/`（Go）、`evals/reports/history/`
- Create: `.github/workflows/nightly.yml`
- Modify: `.github/workflows/ci.yml`（加 mock eval job）

- [ ] **Step 1: 定义任务集（六类，每类 ≥ 8 条，共 ≥ 50 条）**

| 类别 | 目录 | 判定方式 |
|------|------|----------|
| 单工具 | `tasks/single_tool.jsonl` | 期望工具名命中 |
| 多工具编排 | `tasks/multi_tool.jsonl` | 期望工具序列（允许顺序容差） |
| 长任务（≥5 步） | `tasks/long_horizon.jsonl` | 最终产物断言 + 步数上限 |
| HITL 审批 | `tasks/hitl.jsonl` | 是否在正确时机请求确认 |
| 记忆召回 | `tasks/memory_recall.jsonl` | 召回内容命中率 |
| 安全拒答 | `tasks/safety.jsonl` | 不得执行危险动作 |

每条含：`id`、`input`、`expect`（工具/产物/判据）、`max_steps`、`tags`。

- [ ] **Step 2: Mock 回放模式（进 CI 的关键）**

runner 支持 `--mode=mock`：用录制的模型响应序列驱动 Agent，使结果**确定性可复现**。录制格式与 `web` 侧 mock（`web/e2e/helpers/mock-api.ts`）保持一致的思路，避免两套心智模型。

- [ ] **Step 3: 指标与报告**

输出 `evals/reports/history/<timestamp>.json`：

```json
{
  "summary": {"completion_rate": 0.86, "tool_selection_f1": 0.91,
              "avg_steps": 4.2, "avg_cost_usd": 0.013},
  "failures": [{"id": "multi_tool-3", "reason": "wrong_tool",
                "attribution": "prompt|tool|schema|model"}],
  "baseline_diff": {"completion_rate": -0.02}
}
```

- [ ] **Step 4: 门禁与基线**

- CI：mock eval 完成率相对 baseline 下降 > 2pt → 失败。
- baseline 存 `evals/reports/baseline.json`，更新需 PR 说明原因。
- nightly（`.github/workflows/nightly.yml`）：live 模式跑真模型，产出报告并保留 30 天。

- [ ] **Step 5: 首次基线**

在 Task 3/4 完成后跑一次，作为「成熟度补齐前」的基线快照，供 Task 6/7/14 的改动做前后对比。

- [ ] **Step 6: 验证 + Commit**

```bash
cd evals/runner && go test ./... -count=1 && go run . --mode=mock --out ../reports/history/dev.json
```

`feat(evals): task suite and dual-mode runner with regression gate`

---

### Task 14: 结构化 Plan-Execute（G10）

**Files:**
- Rewrite: `framework/agent/plan_agent.go`、Create `framework/agent/plan.go`、`plan_agent_test.go`
- Modify: `portal/internal/chat/agent_builder.go`（`Agent.Mode`）、`portal/internal/service/chat_stream.go`（plan 事件）、`web/src/pages/ChatPage.tsx` + `timelineReducer.ts`

- [ ] **Step 1: 定义结构**

```go
type PlanStep struct {
	ID              string   `json:"id"`
	Goal            string   `json:"goal"`
	SuggestedTools  []string `json:"suggested_tools,omitempty"`
	SuccessCriteria string   `json:"success_criteria"`
	Readonly        bool     `json:"readonly"`
}
type Plan struct {
	Steps []PlanStep `json:"steps"`
}
```

- [ ] **Step 2: 失败测试**

```go
func TestPlanAgent_PlannerOutputMustValidate(t *testing.T)      // 非法 JSON → 修复重试，最多 N 次
func TestPlanAgent_ReplansOnStepFailure(t *testing.T)           // 步骤失败 → replan，上限 2 次
func TestPlanAgent_ReadonlyStepSkipsApproval(t *testing.T)
func TestPlanAgent_FallsBackToReActOnPlannerFailure(t *testing.T)
```

- [ ] **Step 3: 实现**

- planner 用严格 JSON schema 约束输出（复用 Task 5 的校验能力）；失败自动修复重试（≤2 次），仍失败则**回退 ReAct**（而不是失败整个 turn）。
- executor 逐步执行，每步结束做 step verify（基于 `SuccessCriteria` 的轻量判定：工具结果或模型自评），失败触发 replan。
- `Agent.Mode = react | plan`，默认 `react`（保守）；Portal 配置按 agent 选择。
- plan 全量写入 `RunTrace`，SSE 新增 `plan` / `plan_step` 事件。

- [ ] **Step 4: Web 时间线**

plan 面板：步骤列表 + 状态（pending/running/done/failed/replanned）；与现有 `timelineReducer.ts` 的节点合并逻辑对齐。

- [ ] **Step 5: 用 eval 证明增益**

跑 Task 13 的 `long_horizon` 与 `multi_tool`：完成率不下降、平均步数下降（给出具体数字写入计划收尾报告）。

- [ ] **Step 6: Commit**

`feat(agent): structured plan-execute mode with replan`

---

### Task 15: Skill 热重载

**Files:**
- Modify: `framework/skills/index.go`、`framework/skills/index_test.go`
- Modify: `portal/internal/server/*`（skills reload 路由）、`portal/internal/chat/agent_builder.go`

- [ ] **Step 1: 失败测试**

```go
func TestIndex_WatchReloadsOnSkillChange(t *testing.T)  // 新增 SKILL.md → 300ms 内可路由到
func TestIndex_WatchDebouncesRapidWrites(t *testing.T)  // 连续 5 次写 → 只重建 1 次
func TestIndex_ReloadIsAtomic(t *testing.T)             // reload 期间并发查询不返回半成品索引
```

- [ ] **Step 2: 实现**

复用 `memorysearch/builtin.go` 的 fsnotify 模式：增量更新 + 去抖（300ms）+ 原子替换（`atomic.Pointer[Index]`）。默认关闭（配置 `skills.watch_enabled`），避免影响启动行为。

- [ ] **Step 3: Portal 路由**

`POST /api/v1/skills/reload`（手动触发，鉴权同其他管理 API）；技能包上传成功后自动 reload。

- [ ] **Step 4: Commit**

`feat(skills): hot reload skill index via fsnotify`

---

### Task 16: 多渠道出站抽象

**Files:**
- Create: `portal/internal/channel/outbound.go`（接口 + 注册表）
- Modify: `portal/internal/channel/wecom.go`、`wxpusher.go`、`portal/internal/chat/wecom_wiring.go`

- [ ] **Step 1: 接口**

```go
type OutboundChannel interface {
	ID() string
	Send(ctx context.Context, msg OutboundMessage) (DeliveryReceipt, error)
	Validate(cfg ChannelConfig) error
}
```

- [ ] **Step 2: 迁移 wecom / wxpusher 为两个实现**，`send_to_wecom` 工具改为「按 skill/agent 绑定选择 channel」，保留其为 wecom 的兼容别名（避免破坏既有 agent 配置）。

- [ ] **Step 3: 测试 + Commit**

`refactor(channel): extract OutboundChannel interface`

---

### Task 17: Growth phase-2 收口

**Files:**
- Modify: `portal/internal/service/growth_agent_review.go:124`（`TODO(phase-2): full-tools 通用工具集入口待接入`）
- Modify: `portal/configs/config.yaml`（`combined_review_enabled` / `curator_enabled` / `session_end_memory_review_enabled`）

- [ ] **Step 1: 逐项决策**：对每个默认关闭的子能力，要么给出实现 + 证明增益（单测或 eval），要么删除开关并在 `portal/docs/growth-*.md` 标注为实验性/暂缓。

- [ ] **Step 2: 补 full-tools 通用工具集入口**（`TODO(phase-2)`），并确保复盘 agent 的工具面受 `ApprovalPolicy` 约束（复盘 agent 不应有 destructive 能力）。

- [ ] **Step 3: 验证**：`cd portal && go test ./internal/service/... ./internal/biz/... -count=1`，并跑一次真实的 session-end review 冒烟。

- [ ] **Step 4: Commit**

`feat(growth): phase-2 full-tools entry and feature-flag cleanup`

---

# P3：持续度量与文档治理

### Task 18: 成熟度 KPI 面板

**Files:**
- Create: `deploy/grafana/dashboards/maturity.json`（或文档化 PromQL）
- Modify: `portal/internal/server/http.go`（如需补指标）、`gateway/internal/observability/metrics.go`

- [ ] **Step 1:** 定义并落地指标：可用性、P99 延迟（Portal 与 Gateway 分开）、任务完成率（来自 eval + 生产 `turn_trace`）、单任务成本、HITL 触发率、eval 回归通过率、CI 时长。
- [ ] **Step 2:** 阈值告警（示例）：重试率 > 5%、Gateway 5xx > 1%、单任务成本 P95 超预算、eval 完成率跌破 baseline。
- [ ] **Step 3:** 写入 `docs/` 运维说明并链接。

---

### Task 19: 文档与残留治理

**Files:**
- Rewrite: `framework/README.md`
- Create: `docs/adr/`（ADR 目录）
- Create: `docs/runbooks/`
- Delete/Modify: `portal/openapi.yaml`、根 `main.go`、根 `go.mod`、根 `go.sum`

- [ ] **Step 1: 重写 `framework/README.md`**

当前自称 "V0.1 MVP 骨架" 与代码严重脱节（已含多轮 ReAct、护栏、证据门、Skills、MCP 进程池、多层记忆、growth）。按真实能力重写：能力清单、架构图、快速开始、与 Portal 的衔接点。

- [ ] **Step 2: ADR**

补关键决策记录：为何 token 采用粗估 + 校准（而非强制 tokenizer）、为何 Gateway 采用 bot 租约单副本、为何 Plan 模式默认关、为何并行工具先审计再默认开。

- [ ] **Step 3: Runbook**

`docs/runbooks/`: Portal 不可用、企微 WSS 断连/重复订阅、重试风暴与成本飙升、幂等冲突排查、eval 回归失败处置。

- [ ] **Step 4: 清残留**

- `portal/openapi.yaml` 仍是 `Greeter`/`helloworld` 模板 → 重新生成或删除并在 README 指向 proto。
- 根 `main.go` / `go.mod`（`module sixath`）/ `go.sum` 为 GoLand 模板残留 → 删除（确认无脚本引用后）。

- [ ] **Step 5: 验证**

全库搜索 `Greeter`、`helloworld`、`module sixath` 无残留。

---

## 完成定义

| 门槛 | 验收方式 |
|------|----------|
| G1 重试 | 注入 429/5xx/超时单测通过；`turn_trace` 可见 `retry_count`；流式首 token 后不重试 |
| G2 token | 校准误差中位数 < 15%；L0/L2 触发按 token 判定；成本落库 |
| G3 CI | `main` 保护规则生效，四 job 为必需检查，全绿耗时 < 8 分钟 |
| G4 凭据 | `channels.yaml` 不在版本库；`gitleaks` job 通过；凭证已轮换 |
| G5 参数校验 | 非法参数不进入 Execute；`validate_coverage_test` 显示 schema 声明率基线 |
| G6 可观测 | Gateway `/metrics` 可抓取；Gateway→Portal→framework 同 trace |
| G7 分页 | 1000 条会话完整回放无重复/缺失；旧客户端不破坏 |
| G8 评测 | mock eval 进 CI 门禁；`baseline.json` 存在；改动可出量化 diff |
| G9 取消 | 取消 3s 内停、部分结果保留、`goleak` 通过 |
| G10 Plan | 结构化 plan 校验/重试/replan 测试通过；`long_horizon` 步数下降且完成率不降 |

**术语一致性：** 全文「门槛 G1–G10」与上文表格编号一一对应；Task 编号与排期表一致；不再新增未经表格定义的门槛。

---

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| 重试放大成本（长任务尤其明显） | 重试预算 + 流式仅首 token 前重试 + 成本指标告警（Task 18） |
| 并行默认开启引入竞态 | 先审计 `RequiresSequential`，`-race` 单测，保留 `SATH_PARALLEL_TOOLS=0` kill switch，灰度后再删开关 |
| `send_to_wecom` 语义改为「已入队」影响模型判断与既有测试 | 同步更新工具描述、web 文案与测试；在 prompt 中明确「入队 ≠ 送达」 |
| 游标分页破坏 rewind / compact 语义 | Task 11 Step 1 强制先写组合语义测试；UI 保留 compact 分隔条语义 |
| 参数校验一次性打破既有工具 | schema 缺失时跳过 + warn；以 `validate_coverage_test` 作为渐进基线而非硬性 100% |
| Plan 模式损伤已验证的 ReAct 路径 | 默认 `react`；planner 失败回退 ReAct；用 eval 对比后再考虑默认切换 |
| eval 集建模偏差导致指标虚高 | mock + live 双轨；任务集由真实失败案例反哺；指标只用于相对比较 |
| 历史提交清理误伤协作者 | 默认不执行；先轮换凭证（治本）；需用户明确确认后再做并通知全员重克隆 |
| Portal 单测依赖 MySQL 拖慢 CI | 用 `services: mysql:8.0` + 并行 job；必要时拆 `-short` 跳过集成测 |

---

## 附录

**常用验证命令**

```bash
# framework
cd framework && go build ./... && go test ./... -race -count=1

# portal（需 MySQL）
cd portal && go build ./... && go test ./... -count=1

# gateway
cd gateway && go build ./... && go test ./... -race -count=1

# web
cd web && npm ci && npm run test && npm run build && npm run test:e2e

# 全栈烟雾（服务已起）
powershell -File _neo4j_q/verify_inbound_gateway.ps1

# eval（Task 13 之后）
cd evals/runner && go run . --mode=mock --out ../reports/history/dev.json
```

**相关文档**
- 入站 Gateway 设计：[`2026-08-09-inbound-gateway-design.md`](../specs/2026-08-09-inbound-gateway-design.md)
- 企微长连接设计：[`2026-08-09-wecom-bot-gateway-design.md`](../specs/2026-08-09-wecom-bot-gateway-design.md)
- 确认卡 UX：[`2026-07-13-confirm-card-ux-design.md`](../specs/2026-07-13-confirm-card-ux-design.md)
- 并行工具前身：[`2026-07-11-harness-s3-parallel-tools.md`](2026-07-11-harness-s3-parallel-tools.md)
