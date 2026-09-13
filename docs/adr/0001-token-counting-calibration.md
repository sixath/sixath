# ADR-0001：token 计数采用「粗估 + usage 回填校准」，而非强制接入真实 tokenizer

- **状态**：已接受
- **日期**：2026-09-12

## 背景

上下文压缩触发点与成本预算需要 token 计数。可选方案：
1. 接入真实 tokenizer（如 `tiktoken-go`），精确但只覆盖 OpenAI 系；
2. 用 `rune × alpha` 粗估，简单但误差不可见、不收敛。

## 决策

采用 **`TokenCounter` 接口 + 双实现**（`framework/model/token_counter.go`）：

- `ConservativeCounter`：保留 `rune × alpha` 粗估作为 fallback。
- `CalibratedCounter`：provider 拿到 `resp.Usage` 后回填校准，反馈律 `alphaEff ← alphaEff × (actual/estimated)`，一步收敛、不振荡；`ratioEMA` 仅作观测不参与修正。

## 理由

- 跨 provider（DashScope/Ollama）没有统一 tokenizer，强制接入会引入单一厂商耦合。
- 校准把「开环玄学调参」变成「闭环可测量」，且无需改动决策逻辑。
- 反馈律乘「当前生效系数」而非 base，避免跨语言/跨模型偏差累积。

## 后果

- 无 usage 样本时行为与旧版一致（粗估）。
- 代价：需要 provider 层在拿 usage 后调用 `ObserveTokenUsage`；成本精确度依赖样本量（冷启动期仍为粗估）。
