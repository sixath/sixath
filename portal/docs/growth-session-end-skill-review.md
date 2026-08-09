# Growth C2s：会话结束触发技能复盘

**配置**：`growth.session_end_skill_review_enabled`（默认 `false`）

## 行为

**G2（Scheme A）**：`TrySessionEndMemoryReview` / `TrySessionEndSkillReview` 挂在 **ChatSession 结束**，不在 assistant 落库路径。

1. 用户 **删除会话**（`DeleteSession` 成功）后，`ChatService` 调用 `ChatSessionHooks.OnChatSessionEnd`。
2. Growth 注册的 hook 依次执行：
   - **C2** `TrySessionEndMemoryReview`（`session_end_memory_review_enabled`）
   - **C2s** `TrySessionEndSkillReview`（`session_end_skill_review_enabled`）
3. Assistant 落库路径只做阈值计数（`OnAssistantTurn` / `NotifyGrowthAssistantTurn`），**不再**调用 `TrySessionEnd*`。

C2s 置 `pending_skill_review` 的条件：

- 配置已开启；
- 当前 **无** `pending_skill_review`（已达阈值触发的 pending 不重复置位）；
- 本会话有成长活动：`tool_iters_since_review > 0` **或** `turns_since_memory_review > 0`；
- 然后 `growthwake.Wake()` 唤醒 `GrowthWorker`。

与「每 N 次工具成功」阈值触发（G1，默认 N=10，可配置，见 [`growth-g1-nudge.md`](./growth-g1-nudge.md)）**可叠加**：先达阈值会先 pending；未达阈值时，**删除会话**即可触发技能复盘。

## 与 C2 记忆轻检的关系

C2 与 C2s **独立检查** 各自的 pending 位，可同时置 `pending_memory_review` 与 `pending_skill_review`（便于 `combined_review_enabled` 合并 LLM）。

## 配置示例

```yaml
growth:
  llm_review_enabled: true
  review_patch_file: "growth_review_patch.example.json"
  session_end_skill_review_enabled: true
```

## 验收

1. 开启 C2s，启动 backend。
2. 与 Agent 对话：至少 1 次工具成功 + 1 轮 assistant 回复（不必凑满 10 次工具）。
3. **删除该会话**；查库：`pending_skill_review=1`；稍后 worker 写盘并清零。
4. `GET /api/v1/growth/metrics` → `reviews_completed` 增加。

可选：不手动 SQL 置 pending，依赖 C2s（经 DeleteSession）即可触发写盘。

写盘后的 Skill 可由 **自动路由** 在下次对话预注入，见 [`skill-auto-route.md`](./skill-auto-route.md)。

真 LLM 复盘与 `.learnings` 注入见 [`growth-llm-learnings.md`](./growth-llm-learnings.md)。
