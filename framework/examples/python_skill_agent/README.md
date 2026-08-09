# Python Skill 使用示例

本示例演示如何通过 Skills-aware Chat Handler 使用 **python-data-helper** Skill：该 Skill 在 `skills_examples/python-data-helper` 下提供 Python 脚本（如 `scripts/version.py`、`scripts/format_json.py`），在开启脚本执行并允许 `.py` 扩展名后，模型可调用 `execute_skill_script` 执行这些脚本。

## 前置条件

- 已设置 `OPENAI_API_KEY`
- 本机已安装 **python3**（用于执行 Skill 下的 `.py` 脚本）
- 可选：`OPENAI_BASE_URL`、`OPENAI_MODEL`

## 运行方式

在**仓库根目录**执行：

```bash
# 使用默认消息（加载 python-data-helper 并执行 scripts/version.py）
go run ./examples/python_skill_agent

# 自定义问题
go run ./examples/python_skill_agent -message "请加载 python-data-helper，执行 scripts/version.py 并告诉我输出"

# 指定 skills 目录（默认 skills_examples）
go run ./examples/python_skill_agent -dirs skills_examples
```

## 配置说明

示例中已通过代码开启：

- `skills.allow_script_execution = true`
- `skills.script_allowed_extensions = [".sh", ".py"]`

因此 `execute_skill_script` 工具会注册，且可执行 `scripts/` 下的 `.sh` 与 `.py` 文件。Python 脚本由 **python3** 解释器执行。

## 相关 Skill

- **python-data-helper**（`skills_examples/python-data-helper/SKILL.md`）
  - `scripts/version.py`：输出 Python 版本与技能名（无需输入）
  - `scripts/format_json.py`：从 stdin 读 JSON 并格式化
  - `scripts/stats.py`：从 stdin 读数字行并输出简单统计
