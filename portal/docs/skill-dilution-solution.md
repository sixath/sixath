# Skill 提示词稀释问题 - 解决方案设计

## 问题

SKILL.md 文档较长（160+ 行）时，模型对后半部分指令关注不足，导致关键约束（如「必须先 describe_table 和 execute_read」）被稀释，模型仍会直接调用 execute_skill_script 并杜撰参数。

## 设计原则

1. **关键约束前置**：强制规则放在文档最前 20 行内
2. **渐进式披露**：主文档精简，详细内容移至 docs/ 按需读取
3. **主文档控长**：SKILL.md 主体控制在 80 行以内
4. **多位置强化**：核心约束在 3 处以上重复出现
5. **结构化呈现**：用标题、清单、编号让约束更醒目

---

## 方案一：重构 vm_log_analyze 的 SKILL.md 结构

### 新结构（推荐）

```
vm_log_analyze/
├── SKILL.md              # 主文档，~60 行，核心：执行顺序 + 最小示例
└── docs/
    ├── output_format.md  # 输出格式（JSON 示例）按需读取
    └── troubleshooting.md # 故障排查（可选）
```

### SKILL.md 主文档结构（精简版）

```markdown
---
name: vm_log_analyze
description: 根据 TraceId 查询 VM 日志。必须先 describe_table → execute_read → execute_skill_script，禁止跳过。
allowed_tools: [list_tables, describe_table, execute_read, execute_skill_script]
---

# VM 日志分析

## 执行顺序（不可跳过）

1. describe_table(table_name="t_game_virtual_machine_info")
2. execute_read(dsl="SELECT mgr_ipv4_address, id, name FROM t_game_virtual_machine_info ...")
3. 对每条记录的 mgr_ipv4_address 调用 execute_skill_script

**禁止**：跳过 1、2 直接调用 execute_skill_script。mgr_ipv4_address 只能从 execute_read 结果获取，禁止杜撰。

## 三步调用示例

``` 
describe_table(table_name="t_game_virtual_machine_info")
execute_read(dsl="SELECT mgr_ipv4_address, id, name FROM t_game_virtual_machine_info WHERE ...")
execute_skill_script(name="vm_log_analyze", path="scripts/vm_log_analyze.ps1", input='{"mgr_ipv4_address":"<从 execute_read 结果取>","trace_id":"<用户提供>"}')
```

## 参数说明

- trace_id：用户提供
- mgr_ipv4_address：必须来自 execute_read 返回的该列，不能传 vm_id、id

## 前置条件

- Agent 需配置数据源，能访问 t_game_virtual_machine_info
- 需开启 skills.allow_script_execution

## 输出格式

详见 [docs/output_format.md](docs/output_format.md)
```

---

## 方案二：框架级 Skill 结构约定（可选）

为所有 Skill 定义统一结构，便于模型快速定位关键信息：

```markdown
# Skill 名称

## 执行顺序（必读）
（3-5 行，不可跳过的步骤）

## 快速示例
（最小可运行示例）

## 详细说明
（可选，或链接到 docs/）

## 输出/参考
（可选，或链接到 docs/）
```

---

## 方案三：frontmatter 扩展（可选）

在 frontmatter 中增加 `workflow` 字段，供系统提示或工具描述使用：

```yaml
---
name: vm_log_analyze
workflow: "describe_table → execute_read → execute_skill_script"
description: ...
---
```

框架在构建技能说明时，可将 workflow 注入到技能列表或 load_skill 返回的摘要中，强化「必须先执行」的约束。

---

## 实施建议

1. **立即实施**：按方案一重构 vm_log_analyze 的 SKILL.md，将输出格式移至 docs/output_format.md
2. **验证效果**：在相同 Agent 配置下对比重构前后的执行顺序遵守率
3. **推广**：若有效，将方案一作为 skills_examples 的推荐结构，写入 best-practices.md
