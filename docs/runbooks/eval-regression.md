# Runbook：eval 回归失败处置

## 症状

- CI `evals` job 失败：`go test ./...` 不绿，或 `--mode=report -baseline` 门禁 `gate_passed=false`（完成率相对 baseline 下降 > 2pt）。
- 本地 `go run . -mode=report` 报告里 `baseline_diff.completion_rate_delta` 为负且超阈值。

## 排查

1. 看失败分布：报告 `failures[].reason` 与 `attribution`（`prompt | tool | schema | model`）。
2. 定位是否「某类任务整体退化」：`summary.by_category[].completion_rate`。
3. 反查本次改动：prompt / 工具 / 压缩策略 / 参数校验的哪一处影响了任务完成。

## 处置

- 若为**真回归**（改动确实变差）→ 回退改动或修复，重跑 eval 确认回基线。
- 若为**任务集漂移**（工具改名、判据过时）→ 更新任务集 + 同步更新 baseline，并在 PR 说明原因（baseline 更新需 PR 说明，见 `evals/README.md`）。
- 若为**flaky** → 先本地复跑确认非时序抖动，再合并；持续 flaky 的任务移出门禁集。

## 恢复确认

- 本地 `go run . -mode=report -baseline ../reports/baseline.json` 退出码 0，`gate_passed=true`。
- CI `evals` job 全绿。
