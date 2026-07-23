# Skill 创建写前 Schema 校验与质量启发式设计

**日期**: 2026-07-23  
**状态**: Spec review 已通过；待用户确认后进入 writing-plans  
**方案**: 写前硬拦 schema（name + description）+ 质量启发式仅 warn  
**关联**:  
- [harness-engineering-gap-design](./2026-07-11-harness-engineering-gap-design.md)（验证回压 / G4；本设计为技能写路径的第一道传感器）  
- `framework/skills/index.go`（`parseSkillFrontmatter`）  
- `framework/tool/skillops/skill_manager_tool.go`（`skill_manage`）  
- `framework/skills_examples/skills/rca-investigation/SKILL.md`、`harness-fix/SKILL.md`（质量样本）

---

## 1. 背景与问题

Agent 经 `skill_manage` create/edit 写入 SKILL.md 时，当前只做注入扫描（`growth.ScanUserContent`）与路径/补丁语义校验，**不解析 frontmatter**。成功返回 `status=ok` 后：

- 无 `---` frontmatter → `NewIndex` **静默跳过**，技能「写了但列表找不到」  
- YAML 非法 / 缺 `name` → 下一轮建索引才失败  
- 缺 `description` → 可被索引，但 `skills_list` 路由几乎不可用  

用户体感：创建后用不了 → 提示格式不对 → 再改。同会话内也可能反复提交不合规 content。

**本设计只解决技能写路径的「能否入库 + 轻量质量提示」**。通用工具结果契约门、同会话「勿再犯」注入见 §7 backlog，不在本期实现。

---

## 2. 目标与非目标

### 2.1 目标

1. `skill_manage` 在 **propose 与 apply 之前** 校验待写入的 SKILL.md 全文，schema 不合规则 **硬拦、不写盘、不进入 pending**。  
2. 硬规则覆盖 Index **收录必要条件**（合法 frontmatter + 非空 `name`），并额外要求：**`description` 非空（H6）**、**`name` kebab-case（H4）**（产品选择 A；H4/H6 严于今日 Index）。  
3. 质量启发式（选择 B）产出 `warnings[]`，**不阻断** pending/写盘，便于模型自改与人审。  
4. 解析逻辑与 `skills` Index **单源**，避免双份 YAML 规则漂移。

### 2.2 非目标

| 项 | 说明 |
|----|------|
| 通用工具 After 契约校验 | backlog |
| 同会话失败指纹注入 messages | backlog |
| `tags` / `scope` / `allowed_tools` 必填或类型硬拦 | **不做单独校验**：缺省不拦；YAML 能 unmarshal 后，字段归一化与 Index 一致（非法标量导致 unmarshal 失败则走 H2）。不新增「类型不合法」专用 error/warn 码 |
| 经 `skill_manage` 改 frontmatter `name` 完成 rename | **本期不支持**：H5 要求 content `name` == 参数 `name`；改名走 create+delete（或后续独立 epic） |
| 改 confirm 卡交互协议 | 可在 preview/SSE 附带 warnings，不强制新 kind |
| 用 LLM 评判 Skill 好坏 | 本期仅确定性启发式 |

---

## 3. 架构与组件

```text
skill_manage(create|edit|patch|write_file→SKILL.md)
  → ScanUserContent（现有）
  → 合成待写入 SKILL.md 全文
  → skills.ValidateSkillMarkdown(content, expectName)
        ── schema fail ──→ { error, error_code, hint, example }  // 不 pending / 不写盘
  → skills.AssessSkillQuality(meta, body) → warnings[]
  → propose → pending + preview + warnings
     或 apply → ApplyPatchBatch → { status: ok, warnings }
```

| 单元 | 建议路径 | 职责 |
|------|----------|------|
| `ValidateSkillMarkdown` | `framework/skills/validate.go`（新建） | 解析 frontmatter；硬规则；返回 `SkillMeta` + body 或 error |
| `AssessSkillQuality` | 同上 | 仅启发式 warn，永不返回 error |
| 接线 | `skillops`：`proposeSkillManage` / `applySkillManage` | 在注入扫描之后、SavePending / ApplyPatchBatch 之前调用 |
| 解析复用 | 从 `parseSkillFrontmatter` 抽出 content 版（如 `parseSkillFrontmatterContent`） | Index 与校验共用 |

---

## 4. 校验规则

### 4.1 硬拦（`error_code=skill_schema_invalid`）

| # | 规则 |
|---|------|
| H1 | 正文（去 BOM）须以 `---` 开头，且存在合法闭合的 frontmatter（与 Index 一致） |
| H2 | frontmatter YAML 可 `yaml.Unmarshal` |
| H3 | `name` trim 后非空 |
| H4 | `name` 匹配 kebab-case：`^[a-z0-9]+(?:-[a-z0-9]+)*$` |
| H5 | `name` == `skill_manage` 参数 `name`（两侧 trim） |
| H6 | `description` trim 后非空 |

失败响应形态（`err=nil` + map，与现有 soft fail 一致）：

```json
{
  "error": "skill_schema_invalid: description is required",
  "error_code": "skill_schema_invalid",
  "hint": "SKILL.md must start with YAML frontmatter containing name and description.",
  "example": "---\nname: my-skill\ndescription: >\n  Use when …\n---\n\n# My Skill\n"
}
```

### 4.2 软 warn（可写盘 / 可 pending）

长度均按 **Unicode 字符（Go `utf8.RuneCountInString` / `[]rune`）** 计，避免中文误报。

| 码 | 条件（初值，可配置常量） |
|----|--------------------------|
| `desc_too_short` | `description` rune 数 &lt; 40 |
| `desc_not_trigger` | trim 后不以触发语开头（大小写不敏感）。**本期封闭前缀表（仅此三项，扩展另开变更）**：`Use when`、`使用时机`、`当`（覆盖「当…时」类中文触发；不要求匹配结尾「时」） |
| `body_too_short` | frontmatter 后正文 rune 数 &lt; 120 |
| `no_success_signal` | 正文无成功信号子串（大小写不敏感）：`成功`、`Success`、`checklist`、`验收`、`ok:` |

`warnings` 为对象数组，字段：`code`、`message`。

### 4.3 动作覆盖

| action | 是否校验 |
|--------|----------|
| `create` / `edit` | 是（对 `content`） |
| `patch` | 是：读现有 SKILL.md + 应用替换得合成全文后校验（替换语义与 `skillManageToPatches` / Apply 一致） |
| `write_file` | 当 `file_path` 的 **basename** 满足 `strings.EqualFold(base, "SKILL.md")`（与 Index `Walk` 收录规则一致，含 `skill.md` / `Skill.md`）且写入目标落在该 skill 目录下时校验 `file_content` |
| `delete` / `remove_file` | 否 |
| 其它 `write_file` | 否 |

---

## 5. 错误处理与兼容

- Schema 失败：不调用 `SavePending`，不调用 `ApplyPatchBatch`。  
- Warnings：附带在 `pending` 与 `ok` 响应中；不阻断用户 confirm。  
- Index 行为不变：历史已落盘的坏文件仍按今日逻辑跳过或导致 Walk 错误；本设计保证**新写入路径**不再产生无 description / 无 frontmatter 的「假成功」。  
- `growth.ParseSkillNameFromMarkdown` 可继续服务 rename；长期可委托 `ValidateSkillMarkdown` / 共享解析，非本期必须。

---

## 6. 测试要点（TDD）

1. 无 `description` → create 返回 `skill_schema_invalid`，工作区无新文件。  
2. frontmatter `name` 与参数不一致 → 失败。  
3. 合法最小模板（name + description + 短正文）→ propose `pending` 或 apply `ok`，且 `NewIndex` 可 `GetByName`。  
4. description 过短 → 成功路径且 `warnings` 含 `desc_too_short`。  
5. patch 破坏 frontmatter 闭合 → propose/apply 失败。  
6. 回归：注入扫描失败仍优先于 schema（或顺序固定并单测）；pinned / confirm 路径行为不变。  
7. `write_file` 且 `file_path` basename 为 `skill.md`（EqualFold）→ 走与 create 相同的 schema 硬拦。

---

## 7. 风险与 backlog

| 风险 | 缓解 |
|------|------|
| 触发语启发式误伤中文/非英文 Skill | 仅 warn；本期前缀表封闭为三项；不硬拦 |
| patch 合成与真实 Apply 不一致 | 复用 `skillManageToPatches` / 同一替换语义 |
| 与「能入库」之外的质量期望落差 | 文档写明三层评判；实效评测不在本期 |

**Backlog（本设计不实现）**

- V1：通用工具结果契约 / After 校验  
- V2：同会话失败指纹 → 注入「勿再犯」  
- V3：将部分 warn 升级为可配置硬拦  
- V4：Skill 实效评测（有/无 Skill 基线场景）

---

## 8. 成功标准

- 不合规 create **无法**再以 `status=ok`/`pending` 蒙混过关。  
- 合规 Skill 一经确认写入即可被 Index 列出，且必有非空 description。  
- 质量较差但仍合规的 Skill 带 `warnings`，不阻断人审确认。  
- `go test` 覆盖 §6 用例；framework 现有 skill_manage / Index 测试无回归。

---

## 9. 下一步

1. Spec review loop → 用户审阅本文件  
2. 通过后调用 **writing-plans** 产出实施计划（建议路径：`docs/superpowers/plans/2026-07-23-skill-schema-validate.md`）  
3. 实施时以 H1–H6 / warn 码为追溯键
