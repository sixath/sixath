import json
import urllib.request

h = {"Authorization": "Bearer dev-bootstrap-token"}
aid = "e8107fb3-e40a-4207-9d9a-6768847aaf79"

def get(url):
    req = urllib.request.Request(url, headers=h)
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)

agent = get(f"http://127.0.0.1:8000/api/v1/agents/{aid}")
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\agent_e810.json", "w", encoding="utf-8") as f:
    json.dump(agent, f, ensure_ascii=False, indent=2)

print("agent keys", list(agent.keys()))
print("name", agent.get("name"))
ids = agent.get("tool_ids") or agent.get("toolIds") or []
print("tool_ids", ids)
print("mcp", agent.get("mcp_server_ids") or agent.get("mcpServerIds"))
rt = agent.get("runtime_tools") or agent.get("runtimeTools") or {}
print("runtime_tools", rt)

tools = []
for tid in ids:
    t = get(f"http://127.0.0.1:8000/api/v1/tools/{tid}")
    tools.append(t)
    cfg = t.get("config") or {}
    rca = cfg.get("rca") or {}
    print(
        "TOOL",
        t.get("id"),
        t.get("name"),
        t.get("type"),
        "func_path=",
        (cfg.get("func_path") or rca.get("func_path")),
        "query_url=",
        rca.get("query_url"),
        "ds=",
        rca.get("datasource_id") or rca.get("datasourceId"),
        "endpoint=",
        rca.get("endpoint"),
        "roots=",
        rca.get("roots"),
    )

# also list all rca type tools in catalog
all_tools = get("http://127.0.0.1:8000/api/v1/tools?page=1&page_size=100")
print("\ncatalog total", all_tools.get("total"), "items", len(all_tools.get("items") or []))
for t in all_tools.get("items") or []:
    typ = t.get("type")
    cfg = t.get("config") or {}
    rca = cfg.get("rca") or {}
    fp = cfg.get("func_path") or rca.get("func_path")
    if typ in ("rca", "RCA") or (fp and ("rca" in str(fp) or "jaeger" in str(fp) or "es_log" in str(fp))):
        print(" CATALOG", t.get("id"), t.get("name"), typ, fp, "bound=", t.get("id") in ids)
