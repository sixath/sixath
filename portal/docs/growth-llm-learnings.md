# 真 LLM Growth 复盘 + .learnings 管道

## 1. 真 LLM 技能复盘

启用 `growth.llm_review_enabled: true` 后，**优先**使用 `growth.llm`（或环境变量）调用 `NewLLMSkillProposer`，根据 transcript + 技能索引 + learnings 产出 JSON patch 并写盘。

仅当未配置 `llm.model` 且 LLM 客户端构建失败时，才回退 `review_patch_file` 假补丁。

### YAML 示例

```yaml
growth:
  llm_review_enabled: true
  learnings_review_enabled: true
  llm:
    provider: openai
    model: gpt-4o-mini
    api_key: "sk-..."
    base_url: "https://api.openai.com/v1"
    max_transcript_runes: 24000
```

### 环境变量（可替代 YAML llm 块）

| 变量 | 说明 |
|------|------|
| `SATH_GROWTH_LLM_MODEL` | 模型名（必填才启用 LLM） |
| `SATH_GROWTH_LLM_PROVIDER` | openai / dashscope / ollama 等 |
| `SATH_GROWTH_LLM_API_KEY` | API Key |
| `SATH_GROWTH_LLM_BASE_URL` | 可选 Base URL |
| `SATH_GROWTH_LLM_MAX_TRANSCRIPT_RUNES` | transcript 截断 |

启动时 `conf.EnrichGrowthFromEnv` 会合并到 `Bootstrap.Growth`。

### 辅助模型（省配额）

```yaml
growth:
  llm:
    provider: openai
    model: gpt-4o
    api_key: "..."
    auxiliary:
      provider: openai
      model: gpt-4o-mini
      api_key: "..."
```

复盘走 `auxiliary`（见 `newGrowthModelClient` L3）。

---

## 2. .learnings → Skill 管道

`growth.learnings_review_enabled: true` 时，每次技能复盘会读取：

1. `{workspace}/.learnings/`
2. 从 workspace 向上最多 6 层目录的 `.learnings/`（覆盖 monorepo 根目录如 `sixath/.learnings`）
3. 可选 `SATH_LEARNINGS_DIR` 额外目录

聚合 `LEARNINGS.md`、`ERRORS.md`、`FEATURE_REQUESTS.md` 注入复盘 prompt（`framework/growth/learnings.go`）。

LLM 指令（`DefaultSkillReviewSystemPrompt`）要求：优先把 learnings 中可沉淀项写入/更新 `skills/*/SKILL.md`。

### 配置

```yaml
growth:
  learnings_review_enabled: true
  learnings_max_runes: 6000
```

### 与对话闭环

```text
排障 → 人工/Agent 记 .learnings（或 ERRORS.md）
     → C2s 触发 Growth 复盘（含 learnings 摘要）
     → LLM 更新 SKILL.md
     → 下次 auto_route 预注入 Skill
```

---

## 3. 从假补丁切换到真 LLM

1. 注释 `review_patch_file`
2. 配置 `growth.llm` 或设置 `SATH_GROWTH_LLM_*` 环境变量
3. 重启 backend，日志应出现：`growth: using LLM skill proposer model=...`
4. 触发 C2s 或 SQL pending，检查 `skills/` 下文件由 LLM 生成/更新

---

## 4. Agent 写入 learnings：`append_learning`

对话中已注册工具 **`append_learning`**，可将 correction/insight 追加到 `.learnings/LEARNINGS.md` 或 `ERRORS.md`：

```json
{
  "target": "learnings",
  "category": "correction",
  "summary": "一句话总结",
  "details": "可选详情",
  "area": "backend"
}
```

目录解析顺序与 Growth 读取一致（workspace `.learnings` → 上级目录 → `SATH_LEARNINGS_DIR`）。

推荐排障后由 Agent 调用 `append_learning`，再依赖 C2s + LLM 复盘沉淀到 `skills/*/SKILL.md`。

---

## 5. 一键环境（Windows）

```powershell
cd portal
copy .env.growth.example .env.growth   # 编辑 API Key
. .\scripts\setup_growth_env.ps1
go run ./cmd/backend/... -conf ./configs
```

---

## 6. 相关文件

- `framework/growth/learnings.go` · `runner_llm.go` · `skill_review_runner.go`
- `portal/internal/conf/growth_env.go`
- `portal/internal/service/growth_worker.go`
