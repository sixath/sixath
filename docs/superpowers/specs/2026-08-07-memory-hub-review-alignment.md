# 【评审对齐】治理与知识面插件化 + 外部 Memory Hub

**致**: 框架 / 后端负责人  
**日期**: 2026-08-07  
**完整设计**: [`2026-08-07-memory-hub-governance-knowledge-plugins-design.md`](./2026-08-07-memory-hub-governance-knowledge-plugins-design.md)  
**当前状态**: 设计已补齐错误/权威边界(§3.5),待你们确认接口后进入实现规划(P0)。

> 一句话:把「治理」「知识」抽成两个正交、可插拔的 Provider;`local` 是默认实现;外部 Hub 只做 Adapter,不改内核。请重点看下面 3 个接口决策点是否认可。

---

## 1. 需要你们拍板的 3 件事(接口层)

### ① Provider 解析:`Resolve = agent ?? default`,同面不合并

- 全局默认各一个治理面 / 知识面(通常 `local`);Agent 可分别覆盖,已配置则优先。
- **同一面不同时挂多个 Provider**(放弃旧的 local+外部合并 Loadout,避免混权)。

```go
func Resolve(cat Catalog, def Defaults, agent AgentHubConfig) (GovernanceProvider, KnowledgeProvider, error)
// name 不在 Catalog = 硬失败(装配期);绝不静默回落。
```

**要你们确认**:这套 Provider 契约(§3.2 GovernanceProvider/Writer、§3.3 KnowledgeProvider)是否放在 `framework/memory/hub` 包,framework 核心只依赖接口、厂商 SDK 留在 Adapter?

### ② Agent 覆盖落 `RuntimeToolsConfig`,不另开 YAML 面

沿用现有 `RuntimeToolsConfig`(proto/DB,已承载 `hybrid_recall`),新增:

```proto
optional string hub_governance = 10;  // 空 = 用 defaults;非空 = 覆盖为该 provider name
optional string hub_knowledge  = 11;
optional bool   hub_fallback_to_default_on_read_error = 12;
```

**要你们确认**:是否接受在 `RuntimeToolsConfig` 扩展这三个字段(与 `hybrid_recall`/`memory_write_enabled` 同存储路径),而非新开 agents/*.yaml 配置面?

### ③ 状态机映射:不新增 `memory_units` DB 枚举

Provider 面 `AssetStatus`(draft/active/stale/superseded/archived)与 DB `memory_units.status`(active/superseded/deleted)对齐,**draft/stale 走 `metadata.hub_status`**,不改 DB 枚举、不破坏既有 P2-D supersede 语义。

**要你们确认**:这个「不动 DB 枚举、用 metadata 承载 draft/stale」的映射(§4.1)是否可接受?还是倾向直接扩 DB 枚举(需迁移)?

---

## 2. 前一轮评审的 3 个阻塞问题,已在 §3.5 定案

这 3 条是「不写清楚实现者一定各写各的」的接缝,已给出机制级方案:

| # | 问题 | 解决方案(§3.5) | 落地阶段 |
|---|------|-----------------|----------|
| 🔴1 | 「Provider 未注册」和「运行时不可达」混为一谈,导致外部 Hub 宕机被误当配置错误、拒启动 Agent | **两阶段两类失败**:装配期 name 缺失→硬失败;运行时 RPC 不可达→读 fail-open(可配回落)/写 fail-closed;**CheckAccess 不可达按拒绝、不放行** | P0 |
| 🔴2 | `BindAssets` 的 `AssetRef.Hub` 由 UI 填,可能把 local 资产 bind 到 tencent Agent,无机制拦截 | **`enforceHub` 接口层强制 invariant** + `ErrHubMismatch`:写入口第一件事校验 `Hub==Resolve(agent).Name()`,不符即拒,绝不落库/发远程。UI 前置校验仅改善体验、非边界 | P0 |
| 🔴3 | 绑定外部 Hub skill = 远程代码执行,原方案只有一句话 | **物化流程**:只读正文落盘缓存→hash+签名校验(无签名默认落 draft 需人工确认)→version/hash 变更重拉+**重过 procedural 五门闸**→失效标 stale 不静默顶替。**此条不落地,P2 外部 Adapter 不上线** | P2(前置条件) |

---

## 3. 权威边界(需后端认可的红线)

- **会话 / 轨迹 / 压缩:始终本地**,不受 Provider 切换影响。
- **资产 / 知识:当前 Agent 解析后的 Provider**。
- **写永远 fail-closed**,与 Prefetch 读 fail-open 分离。
- **禁止**把外部资产写进 local DB 冒充权威(由 `enforceHub` 机制保证)。

---

## 4. 一期(P0)范围与不做项

**P0 做**:`hub` 接口类型 + Catalog/Defaults/Resolve + §3.5.1 失败分流 + §3.5.2 `enforceHub` + LocalGovernance/LocalKnowledge + 表驱动单测(默认/只覆一面/全覆/未注册硬失败/Hub 不符拒绝)。

**一期明确不做**:本地↔外部双向 sync;完整 Wiki/CodeGraph;单 Agent 多治理 Provider;`KnowledgeProvider.PrefetchHints`(YAGNI,不放空方法);`Confidence` 参与过滤(一期恒 0.5 占位)。

---

## 5. 请回复的问题

1. §1 的 3 个接口决策点(Provider 契约位置 / proto 字段扩展 / 状态机映射)是否认可?有异议的点请指出。
2. §3.5 三个阻塞方案是否覆盖你们担心的失败模式?尤其 🔴3 Skill 物化的信任校验是否够(签名从哪来、谁维护)?
3. 复用既有系统的假设是否准确:procedural 五门闸(`CommitProceduralRepair`)、D2 fail-closed(`LLMSemanticConflictResolver`)、`RuntimeToolsConfig.hybrid_recall`、`skills_dirs`/`load_skill`、`memory_units.status`——这些引用均已对照代码核实存在,若有近期改动请提醒。
4. P0 是否可以按上面范围开工,还是需要先补哪块?

确认后我据此起草 `docs/superpowers/plans/` 下的实现计划。
