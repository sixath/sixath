# sixath-framework Skill · 用户安装说明

教 AI Agent 开发、扩展 **github.com/sixath/framework**，并指导如何编写 **framework 运行时可直接执行** 的 Skill（`load_skill` / `execute_skill_script`）。

## 两种使用模式

| 模式 | 说明 |
|------|------|
| **Framework 运行时** | `config.yaml` 的 `skills_dirs` 包含 `skillpack`，Agent 通过 `skill_view("sixath-framework")` 加载 |
| **外部 Agent（Cursor 等）** | 安装到 `~/.cursor/skills/`，对话 `@sixath-framework` |

## 安装

### Skills CLI

```bash
npx skills add <owner>/sixath --skill sixath-framework -g -y
# 或仅 framework 子目录
npx skills add ./framework/skillpack --skill sixath-framework -g -y
```

### 一键脚本

```bash
bash framework/skillpack/install.sh
powershell -ExecutionPolicy Bypass -File framework/skillpack/install.ps1
```

## Framework 运行时配置

```yaml
skills:
  skills_dirs:
    - skillpack
    - skills_examples
  allow_script_execution: true
  script_allowed_extensions: [".sh", ".ps1"]
```

验证：

```
execute_skill_script("sixath-framework", "scripts/verify.ps1")
```

## 包内文件

- `SKILL.md` — Agent 主指令
- `authoring.md` — 如何写 framework 可执行 Skill
- `reference.md` — 包地图、CLI、设计文档索引
- `scripts/verify.*` — 跑 `go test ./...`

## 前置条件

- Go 1.25+（见 `framework/go.mod`）
- 克隆 monorepo 或单独 clone framework 模块
