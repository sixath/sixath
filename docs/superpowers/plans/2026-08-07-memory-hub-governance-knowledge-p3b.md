# Memory Hub — P3b Local Wiki + Simple CodeGraph

> Nested: `framework/`, `portal/`. **Do not commit unless asked.**

**Goal:** 把 P3a 骨架落地为可搜的本地索引，并在 Portal 按环境变量接线。

## Scope

- [x] `DirWiki`：扫描目录下 `.md/.markdown/.txt/.mdx`，子串检索；`knowledge_read` 按相对路径读全文（有上限）
- [x] `DirCodeGraph`：扫描源码树，路径 + 简易符号正则（非 AST / 非依赖图）；Capabilities 仍用 `code_graph`
- [x] Portal `InitLocalMemoryHub`：`SATH_HUB_WIKI_ROOT` / `SATH_HUB_CODEGRAPH_ROOT` 存在则注入后端
- [x] 默认 `knowledge_search` sources **仍不含** wiki/codegraph（显式 `source=`）
- [x] 单测 + `memory-integration.md` / 本计划

## Out of scope

完整 Wiki ingest 产品、CodeGraph sync、真实 Tencent Adapter、UI 管理索引根目录。
