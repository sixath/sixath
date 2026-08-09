# Agent 评测 Harness 设计

**日期**: 2026-04-24
**工作区**: `D:\workspace\skills\archive-move-ops`
**状态**: 可进入计划阶段

## 目标

新增一套可复用的本地评测 harness，用来批量运行并检查面向 skill 的用例；第一批先以 `archive-move-ops` 作为示例适配器。

这套 harness 必须满足：

- 支持稳定的批量模式，用于基于本地 fixture 的回归检查
- 为更具解释性的半交互模式预留清晰扩展路径
- 第一版优先采用规则判分
- 不依赖 SSH 或实时日志访问
- 能将重复失败和后续改进点沉淀到 `.learnings/`

## 范围

### 范围内

- 一种通用的 case 格式，可用于评测脚本驱动型 skill
- 一个能够执行本地 PowerShell 目标并捕获结果的 runner
- 面向结构化断言的规则判分器
- 机器可读和人类可读两种报告输出
- 一个带代表性示例 case 的 `archive-move-ops` 适配器
- 仅执行本地 fixture

### 范围外

- 真实 SSH 执行
- 生产实时日志访问
- 第一版中的 LLM-as-judge 判分
- 带远端副作用的完整流程回放
- CI 集成

## 用户结果

完成后，维护者应当能够：

1. 在 `cases/` 下新增一个 case 文件
2. 用一条命令评测所有本地 case
3. 看到哪些 case 通过、哪些失败，以及失败原因
4. 查看每个 case 的详细报告
5. 将同一套 harness 结构复用于其他 skill

## 对比过的方案

### 方案 1：纯脚本式 Harness

使用一个或两个 PowerShell 脚本遍历 case，并在脚本中直接做断言。

优点：

- 首次实现最快
- 组件最少

缺点：

- 执行、判分、报告之间边界不清
- 后续扩展半交互模式会更困难
- 跨 skill 复用性较差

### 方案 2：分层插件式 Harness

将 harness 拆成 case 加载、执行、判分、报告几个层次，同时保持实现轻量且仅本地运行。

优点：

- 最适合复用到多个 skill
- 未来扩展时边界更清晰
- 批量模式和半交互模式可以共享同一套结果模型

缺点：

- 相比纯脚本方案，前期会多一些骨架结构

### 方案 3：先做流程回放

围绕 investigator 的操作步骤建模，让 harness 按顺序回放 skill 的预期工作流。

优点：

- 最接近真实排障流程
- 对流程正确性的覆盖更强

缺点：

- 第一版实现更重
- 对输出回归检查来说不如方案 2 直接

## 决策

选择 **方案 2：分层插件式 Harness**。

这个方案既能为第一版提供足够清晰的结构，也不会让本地 fixture-only 版本变得过重。

## 架构

### 顶层目录布局

```text
cases/
  archive-move-ops/
    *.json

harness/
  adapters/
  judges/
  reporters/
  run-harness.ps1
  lib.ps1

reports/
  latest/
  history/

.learnings/
  LEARNINGS.md
  ERRORS.md
  FEATURE_REQUESTS.md
```

### 核心组件

#### Case Loader

从 `cases/` 读取 case 文件，校验必要字段，并将其归一化为统一的内部结构。

#### Adapter

把某种 case 类型映射成具体的本地命令调用。

第一批适配的 `archive-move-ops` 脚本包括：

- `parse-dispatch-log.ps1`
- `build-followup-commands.ps1`
- `build-entry-commands.ps1`
- `build-investigation-report.ps1`
- 可选的 `build-flow-investigation-template.ps1`

#### Runner

执行解析后的本地 PowerShell 命令，并捕获：

- 命令行
- 退出码
- stdout
- stderr
- 耗时等时序元数据

#### Judges

对执行结果应用规则断言。

第一版支持的 judge 类型：

- `exit_code`
- `contains_text`
- `not_contains_text`
- `json_field_equals`
- `json_field_exists`
- `report_section_exists`

#### Reporter

负责输出：

- 一个包含通过/失败统计的批量汇总
- 每个 case 的详细结果文件，带证据和失败检查项
- 一个稳定的 `reports/latest/` 快照，便于快速查看

#### Learning Sink

当某个 case 以值得追踪的方式失败时，向 `.learnings/` 追加简短记录。

第一版行为：

- 只有 harness 执行失败或重复断言失败时，才向 `.learnings/ERRORS.md` 追加错误摘要
- 只有当 harness 已经识别出某类失败模式时，才向 `.learnings/LEARNINGS.md` 追加学习项
- 暂不自动写入 feature request

## Case Schema

case 使用 JSON 存储，便于 diff，也便于 PowerShell 解析。

### 必填字段

```json
{
  "id": "archive.parse-dispatch.basic-json",
  "skill": "archive-move-ops",
  "type": "parse_dispatch_log",
  "description": "将 Worker.startSyncDispatch 日志行解析为 JSON 字段",
  "input": {},
  "expect": {
    "judges": []
  }
}
```

### 推荐结构

```json
{
  "id": "archive.followup.basic-route",
  "skill": "archive-move-ops",
  "type": "build_followup_commands",
  "description": "根据一条 dispatch 日志生成源区和目标区 manager 检索命令",
  "tags": ["local", "fixture", "smoke"],
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)"
  },
  "expect": {
    "judges": [
      {
        "kind": "exit_code",
        "equals": 0
      },
      {
        "kind": "contains_text",
        "target": "stdout",
        "value": "archiver-manager"
      },
      {
        "kind": "contains_text",
        "target": "stdout",
        "value": "400"
      },
      {
        "kind": "contains_text",
        "target": "stdout",
        "value": "301"
      }
    ]
  }
}
```

### Case 类型映射

- `parse_dispatch_log`
- `build_followup_commands`
- `build_entry_commands`
- `build_investigation_report`
- `build_flow_investigation_template`

每种 case 类型都会映射到一个 adapter 函数，这个函数需要知道：

- 目标脚本路径
- 参数映射规则
- stdout 应按文本还是 JSON 处理

## 数据流

1. 操作者运行 `harness/run-harness.ps1`
2. loader 在 `cases/` 下发现 case 文件
3. loader 校验并归一化每个 case
4. adapter 解析出目标脚本和参数
5. runner 本地执行命令
6. judges 对捕获结果进行判分
7. reporter 生成汇总和单 case 详情
8. learning sink 视情况追加失败摘要

## 模式

### 批量模式

这是第一版的主模式。

行为：

- 运行全部 case，或按条件筛选的子集
- 只要有任意 case 失败，进程就返回非零退出码
- 写出稳定的汇总报告

### 半交互模式

这是一个预留扩展方向，不是第一版的主要投入点。

计划行为：

- 运行单个 case 或筛选后的少量 case
- 在终端内打印更详细的 judge 解释
- 指向同一套详情文件

关键约束是：两种模式必须共享同一套内部结果模型。

## `archive-move-ops` 示例覆盖

第一批示例套件应覆盖：

1. 将 dispatch 日志行解析为 JSON
2. 根据 dispatch 日志行生成 follow-up 命令
3. 根据 `traceId` 生成 entry 命令
4. 基于 fixture 日志输入生成 investigation report 模板

初始目标数量：

- 3 到 5 个正向 case
- 如果现有脚本已经有稳定的失败行为，再补 1 到 2 个负向或非法输入 case

## 错误处理

harness 必须区分：

- harness 基础设施失败
- 脚本执行失败
- 断言失败
- case schema 非法

这种区分必须出现在报告里，让操作者能判断问题是在 harness、skill 脚本，还是 case 定义本身。

## 报告

### 批量汇总

包含：

- 运行时间戳
- case 总数
- 通过数
- 失败数
- 跳过数
- 总耗时
- 失败 case 的 id 列表

### 单 Case 详情

包含：

- case 元数据
- 解析后的命令
- 退出码
- stdout 和 stderr 摘要
- judge 结果
- 总体结论
- 失败分类

## 测试策略

实现过程遵循 TDD。

第一批 failing tests 需要先锁定：

1. case 发现和 schema 校验
2. runner 结果对象结构
3. 至少一个文本型 judge
4. 至少一个 JSON 字段型 judge
5. 报告汇总生成

现有脚本测试依旧是脚本行为的事实来源。harness 自己的测试聚焦于：

- 编排
- 归一化
- 判分
- 报告

## 实施阶段

### 阶段 1：Harness 骨架

- 新增 harness 目录结构
- 新增 case schema 支持
- 新增 runner 结果对象

### 阶段 2：Judge 与报告

- 新增初始规则判分器
- 新增汇总与详情报告写入器

### 阶段 3：`archive-move-ops` 适配器

- 将初始 case 类型映射到现有脚本
- 新增本地示例 case

### 阶段 4：Learning 集成

- 向 `.learnings/` 追加精简失败记录
- 保持日志保守且仅限本地

## 风险与缓解

### 风险：过度贴合 `archive-move-ops`

缓解：

把 adapter 逻辑与 loader、runner、judges、reporters 分离。

### 风险：文本断言过脆

缓解：

对 JSON 输出优先做字段级断言，对报告优先做 section 级检查；只有在输出片段足够稳定时才使用文本包含断言。

### 风险：learning 日志噪声过大

缓解：

只记录已分类且有用的失败；默认不把完整命令输出直接写入 `.learnings/`。

### 风险：PowerShell 的 JSON 与编码不一致

缓解：

统一捕获文本格式，并且只在 case 类型明确需要结构化输出时才解析 JSON。

## 第一版非目标

- 远端主机编排
- 压缩日志抓取
- 多步远端流程回放
- 模型驱动判分
- 自动创建 issue

## 已确认的问题

- 目标对象：通用 harness 加 `archive-move-ops` 示例
- 主模式：先做批量模式，半交互后续补
- 判分方式：先规则判分，流程回放后续扩展
- 输入来源：第一版只用本地 fixture

## 成功标准

当满足以下条件时，第一版算成功：

1. 维护者能通过一条本地命令评测所有 harness case
2. 失败结果能清晰区分是 schema、执行还是断言问题
3. 至少有一个 `archive-move-ops` 示例 case 覆盖解析、命令生成和报告生成
4. 报告写入到可预期的位置
5. 设计上为半交互检查和未来 skill adapter 留出清晰扩展路径
