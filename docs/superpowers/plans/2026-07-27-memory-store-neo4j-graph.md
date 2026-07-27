# MemoryStore P2-I Neo4j Graph Plan

**Spec:** `docs/superpowers/specs/2026-07-27-memory-store-neo4j-graph-design.md`

1. `framework/memory/graph_store.go` + fake tests
2. `framework/memory/neo4j_graph_store.go` + runner-interface tests
3. Facade Graph hooks (Invalidate + Recall Expand/RRF)
4. `graph_extract.go` / GraphPipeline + LLMGraphExtractor
5. Config `MemoryGraph` + Portal `memory_graph.go` notify
6. Docs + facade §8.5 backlog
