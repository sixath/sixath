# Trace2Skill 深度分析

> **论文**：Trace2Skill: Distill Trajectory-Local Lessons into Transferable Agent Skills  
> **arXiv**：[2603.25158](https://arxiv.org/abs/2603.25158)  
> **机构**：ETH Zürich、北大、浙大 + 阿里 Qwen 大模型应用团队  
> **代码**：[Qwen-Applications/Trace2Skill](https://github.com/Qwen-Applications/Trace2Skill)

---

## 1. 核心问题：Skill 从哪来、怎么写才靠谱？

LLM Agent 越来越依赖 **Skill**（可复用的 `SKILL.md` + 脚本/参考文件目录），但两条常见路径都有明显短板：

| 路径 | 问题 |
| --- | --- |
| **人工写 Skill** | 不 scale；且同一 Skill 对不同模型效果差异大（Anthropic 官方 xlsx skill 对 122B 有效，对 35B 反而有害） |
| **纯参数知识生成** | 缺少真实执行中的失败模式、workaround、操作细节，往往接近「无 Skill」 |

现有「从经验中学习」的方法又分两类，各有缺陷：

- **检索式记忆**（ReasoningBank、Reflexion 等）：测试时检索 episodic memory，碎片化、依赖检索质量
- **顺序在线编辑**（每来一条 trace 就改一次 Skill）：顺序依赖、知识分散、后期 patch 受前面编辑影响

**Trace2Skill 的核心洞察**：人类专家不是「来一条改一条」，而是**批量看轨迹 → 归纳共性 → 写一份紧凑 SOP**。论文把这一步形式化为 **并行归纳 + 层次合并**。

---

## 2. 方法架构：三阶段流水线

```
Stage 1 轨迹采集 → Stage 2 并行 Patch 提案 → Stage 3 层次合并 → 单一可移植 Skill 目录
```

### Stage 1：轨迹生成

- 固定 Agent π_θ + 初始 Skill S₀，在 evolve 集上 rollout
- 每条轨迹含：query、推理/工具历史、输出、对错标签
- **并行采集**，与后续 patch 解耦

### Stage 2：并行 Patch 提案（关键设计）

- **Error Analyst A⁻**：ReAct 循环，可 inspect 产物文件、对照 ground truth、**验证修复**后再提 patch；无法因果解释的 failure 丢弃
- **Success Analyst A⁺**：单轮提取可复用成功模式
- 128 个子 Agent 并行，产出 patch pool P = P⁻ ∪ P⁺

**不对称设计有深意**：失败需要根因诊断（agentic），成功只需模式抽象（轻量）。

### Stage 3：层次合并

- 每轮最多 B_merge=32 个 patch 合成一个，共约 ⌈log₃₂|P|⌉ 层
- Merge 算子 M 负责：去重、冲突消解、保留非重叠洞察
- **归纳偏置**：多轨迹独立提出相似修改 → 视为系统性规律，保留为通用 SOP；剔除 idiosyncratic 编辑
- 输出 **单一 Skill 目录**，推理时直接加载，**无需 test-time retrieval**

### 两种模式

1. **Skill Deepening**：从人工 Skill（如 Anthropic xlsx）出发增强
2. **Skill Creation**：从 LLM 参数知识生成的弱 draft 出发构建

---

## 3. 实验设计与主要结果

### 3.1 电子表格（主战场）

- **Evolve**：SpreadsheetBench-Verified 200 题
- **Test**：另 200 题 + 全量 Soft/Hard + OOD（WikiTableQuestions、HiTab 转 spreadsheet 格式）
- 模型：Qwen3.5-122B-A10B / 35B-A3B，100% self-evolution（同一模型采轨迹、写 patch、合并）

**亮点数据**：

- 35B 轨迹演化出的 Skill，给 **122B 用户** 在 WikiTQ 上 **+57.65 pp**（Creation +Error）
- Human-Written xlsx：122B 48.33% Vrf → Trace2Skill +Combined 可达 ~70% 量级
- **小模型写的 Skill 也能帮大模型**；跨模型族（Gemma-4、GPT-5.5-high ↔ Qwen）同样有效

### 3.2 跨域验证

| 领域 | Evolve 集 | OOD 测试 | 主要增益 |
| --- | --- | --- | --- |
| 数学 | 400 DAPO | DAPO-Test + AIME'26 | +Error 最稳，122B 用户 D-Test +4~6 |
| DocVQA | 50 例（移出测试集） | 5299 例 | +Combined ANLS +0.25，Acc +22% |
| PDF/PPTX/DOCX | 各域 source traces | 独立 held-out | +7.8~16.3 pp |

### 3.3 与 concurrent 系统 head-to-head

同 base model（Qwen3.5-122B）、同 benchmark 切片：

- XSkill：23.0 Vrf
- SkillGen：27.5
- EvoSkill（React harness）：59.5
- **Trace2Skill +Combined：69.83**

---

## 4. 消融分析：三个设计选择的因果证据

论文 §4.1 在**同一 trace pool、同一 harness** 下做 apples-to-apples 对比，这是论证质量较高的部分。

### 4.1 并行合并 vs 顺序编辑

| 方法 | 122B Vrf | 耗时 |
| --- | --- | --- |
| Seq-B=1（每条 trace 改一次） | 61.83 | ~60 min |
| Seq-B=4 | 59.00 | ~15 min |
| **Parallel（本文）** | **65.83** | **~3 min** |

并行在质量与效率上双优；顺序编辑有**顺序依赖**和**中间状态污染**问题。

### 4.2 单一 Skill vs ReasoningBank 检索

同轨迹池下，+Combined consistently 优于 top-1 embedding 检索（122B Vrf 69.83 vs 56.00）。

**压缩成静态 SOP** 比 **测试时捞记忆** 更稳——尤其 OOD query 与 evolve 集语义距离大时，检索几乎不可用。

### 4.3 Agentic Error Analysis vs 单次 LLM 读 log

- Agentic A⁻ 在 33 个共有 error case 中，与 log-only 仅 **12.1% 强一致**
- log-only 把 parse error 当主因的比例 **57% vs 14%**（agentic 会验文件、发现输出其实正确）
- 结果：agentic patch 更偏「verified failure mechanism」，跨模型/OOD 迁移更好

### 4.4 Patch 价值是组合性的，不是可贪心叠加的

Greedy 逐 patch 验证选择：曲线很快 plateau，**低于 full aggregation**。

原因：

1. **Patch-irrelevant regression**：修 A 题同时搞砸 B 题
2. **Semantic overlap**：多 patch 重复同一主题（如 recalc、验证清单）

→ 支持 **holistic merge** 而非 greedy selection；BO 子集选择略优但验证成本极高。

### 4.5 学到的 SoP 示例（电子表格）

从 323 个 patch 归纳出的高频 SOP：

1. **公式重算 + write-back 验证**（55.1% patch 引用）— `recalc.py` + `data_only=True`
2. **openpyxl 优于 pandas.to_excel()**（54.8%）— 保留公式与 named range
3. **显式 read-back 验证**（42.7%）
4. **结构编辑安全**（16.4%）— 降序删行、先 copy 再改

低频 quirk 自动路由到 `references/`，主 `SKILL.md` 保持通用 SOP——与 Cursor/Anthropic Skill 的 progressive disclosure 设计一致。

---

## 5. 优点（值得借鉴的工程与科研点）

1. **问题定义清晰**：Skill evolution 形式化为 S* = E(S₀, D_evolve)，test 集 disjoint，不更新 θ
2. **Many-to-one 归纳** 对齐人类写文档方式，且可并行，wall-clock 约 3 min vs 60 min
3. **Transfer 证据充分**：跨 scale、跨 family、跨 task（spreadsheet → table QA）、跨 modality
4. **Error analyst 的 agentic 验真** 是质量关键，不是 prompt 花活
5. **与 ReasoningBank / 顺序编辑 / greedy patch 的对比** 在同一 trace pool 上，因果链较干净
6. **开源 + 20k GPU-hour 规模实验**，附录 prompt/diff 示例完整

---

## 6. 局限与可质疑点（批判性阅读）

### 6.1 Evolve / Test 划分与过拟合

- SpreadsheetBench 仅 400 verified，evolve/test 各 200；Skill 高度针对 xlsx 工具链（openpyxl、recalc.py）
- OOD（WikiTQ/HiTab）仍是 **table 结构任务**，语义距离有限；真正 distant OOD（如代码 repo 操作）未充分验证

### 6.2 Self-evolution 循环

- 100% 同模型 author=user；cross-model 主要是 **Skill 迁移**，不是 cross-model trace 归纳的系统性研究

### 6.3 Merge 算子仍是 LLM

- 合并质量依赖 thinking mode 的 Qwen；冲突消解、prevalent pattern 识别无形式化保证
- Guardrails（reject missing file、line-range conflict）是工程补丁，论文未深入分析 merge failure mode

### 6.4 成本与可扩展性

- Stage 2：128 并行 analyst + 全轨迹；Stage 3：层次 merge
- 附录承认 BO patch 选择需要大量 validation rollout；默认 **apply all patches** 可能引入噪声

### 6.5 Baseline 公平性

- Head-to-head 需适配各系统到 open Qwen + ReAct，whole-system 分数不能替代 apples-to-apples 因果 claim
- Anthropic skill-creator + Opus 4.6 在 Vrf 上平均弱于 Trace2Skill，但单格有波动

### 6.6 Skill 负迁移

- 论文强调 Human-Written 对 35B 有害，evolved skill 改善多数情况，但未系统刻画 **何时 Skill 会 hurt**

---

## 7. 与 Agent 生态的定位

**Trace2Skill 的独特卖点**：

- 产物是 **可版本控制、可跨部署加载的 Skill 目录**，不是 memory index
- **归纳压缩** 而非 nearest-neighbor reuse
- 强调 **portability**（换模型、换 benchmark 仍有效）

对 **sixath framework**（ReAct、SKILL.md progressive disclosure、L0/L1/L2 context）而言，这篇论文直接相关的是：

- **Skill 应从 trajectory 蒸馏，而非只靠 parametric 或纯人工**
- **并行 merge 比在线顺序改 SKILL.md 更稳**
- **Error path 需要 agentic 验真**（inspect artifact、rerun validation）——可纳入 skillpack 或 evolution 工具链
- **references/ 分层** 与 framework 现有 Skill 结构一致

---

## 8. 若要在 sixath 中落地：可操作的启示

| 论文组件 | 可迁移做法 |
| --- | --- |
| 并行 patch pool | evolve 集 batch rollout → 多 worker 提 patch，避免单线程改 Skill |
| Error vs Success 双 analyst | 失败走 ReAct+验真；成功轻量 pattern extract |
| Hierarchical merge | log_B N 轮 merge，batch=32，prefer prevalent patterns |
| Diff + guardrails | 对 SKILL.md 做结构化 edit，拒绝冲突/缺文件 |
| 评估协议 | evolve/test 严格 disjoint；报告 ID+OOD 平均（Avg） |
| 避免 test-time retrieval | 若已有 L2/memory，对比「蒸馏进 Skill」vs「检索 memory」 |

**不建议盲目照搬的部分**：128 并行 sub-agent、20k GPU-hour 规模——可先在小 evolve 集（50–100 task）验证 merge 是否优于 sequential edit。

---

## 9. 一句话总结

Trace2Skill 把「从 Agent 执行轨迹写 Skill」建模为 **并行轨迹分析 → 轨迹级 patch → 层次归纳合并 → 单一可移植 Skill 目录**，用大量实验证明：这样得到的不是轨迹备忘录，而是 **跨模型、跨任务可复用的 SOP**；核心杠杆是 **并行 many-to-one 合并**、**agentic 错误根因分析**，以及 **静态 Skill 优于 episodic retrieval**。
