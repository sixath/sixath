# Sixath Framework · Agent Skill 包

可 **被 Sixath framework 运行时执行**（`load_skill` / `skill_view` / `execute_skill_script`），也可 **安装到 Cursor / Claude Code / Codex** 供外部 Agent 开发 framework 时使用。

## 包结构

```
skillpack/
├── README.md              # 本文件
├── install.sh / install.ps1
└── sixath-framework/      # 可被 framework 扫描的 Skill
    ├── SKILL.md           # 主指令（必需）
    ├── README.md          # 用户安装说明
    ├── agents.md          # 跨 Agent 安装
    ├── reference.md       # 包地图、测试、设计文档索引
    ├── authoring.md       # 编写 framework 可执行 Skill 规范
    └── scripts/           # execute_skill_script 可调用
        ├── verify.sh
        └── verify.ps1
```

## 在 Framework 中启用（运行时加载）

`config.yaml`：

```yaml
skills:
  skills_dirs:
    - skillpack
    - skills_examples
  allow_script_execution: true
  script_allowed_extensions: [".sh", ".ps1"]
  script_timeout_seconds: 120
```

启动 Skills-aware Agent 后，模型可：

```
skill_view("sixath-framework")
execute_skill_script("sixath-framework", "scripts/verify.ps1")
```

## 给其他用户安装（外部 Agent）

```bash
# GitHub（替换 owner/repo）
npx skills add <owner>/sixath --skill sixath-framework -g -y

# 本地
npx skills add ./framework/skillpack --skill sixath-framework -g -y

# 脚本
bash framework/skillpack/install.sh
```

## 前置条件

- Go 1.25+（见 `framework/go.mod`）
- 开发 framework：`go test ./...` 在 `framework/` 目录执行
