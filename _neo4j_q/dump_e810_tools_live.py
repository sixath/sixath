# -*- coding: utf-8 -*-
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
h = {"Authorization": "Bearer dev-bootstrap-token"}
aid = "e8107fb3-e40a-4207-9d9a-6768847aaf79"
base = "http://10.86.32.78:8000"

def get(url):
    req = urllib.request.Request(url, headers=h)
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)

agent = get(f"{base}/api/v1/agents/{aid}")
print("name", agent.get("name"))
print("model", (agent.get("modelConfig") or agent.get("model_config") or {}).get("model"))
print("workspace", agent.get("workspace"))
ids = agent.get("toolIds") or agent.get("tool_ids") or []
print("toolIds", ids)
print("mcp", agent.get("mcpServerIds") or agent.get("mcp_server_ids"))
print("runtime", agent.get("runtimeTools") or agent.get("runtime_tools"))

for tid in ids:
    t = get(f"{base}/api/v1/tools/{tid}")
    cfg = t.get("config") or {}
    rca = cfg.get("rca") or {}
    print(
        "TOOL",
        t.get("id"),
        t.get("name"),
        t.get("type"),
        "func=",
        cfg.get("func_path") or rca.get("func_path"),
        "ds=",
        rca.get("datasource_id") or rca.get("datasourceId") or cfg.get("datasource_id"),
        "default_index=",
        rca.get("default_index") or rca.get("defaultIndex"),
        "roots=",
        rca.get("roots") or cfg.get("roots"),
    )

# mcp servers
try:
    mcps = get(f"{base}/api/v1/mcp-servers?page=1&page_size=50")
    items = mcps.get("items") or mcps.get("data") or []
    print("\nMCP catalog n=", len(items), "keys", list(mcps.keys())[:10])
    for t in items:
        print(" MCP", t.get("id"), t.get("name"), t.get("transport"), t.get("command") or t.get("endpoint"))
except Exception as e:
    print("mcp list fail", e)
    try:
        mcps = get(f"{base}/api/v1/mcps")
        print("mcps alt keys", list(mcps.keys())[:10])
    except Exception as e2:
        print("mcp alt fail", e2)
