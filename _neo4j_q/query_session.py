#!/usr/bin/env python3
"""Query Neo4j for session graph edges. Password read from live agent_extra.yaml."""
import base64
import json
import re
import urllib.request
from pathlib import Path

SID = "b73cf880-f42f-497a-8672-3fd39414fb2a"
CONF = Path(r"E:/configs/sixath/portal/agent_extra.yaml")

text = CONF.read_text(encoding="utf-8")
m = re.search(r'password:\s*"([^"]+)"', text)
if not m:
    raise SystemExit("password not found in agent_extra.yaml")
passw = m.group(1)

auth = base64.b64encode(("neo4j:" + passw).encode()).decode()
body = json.dumps(
    {
        "statements": [
            {
                "statement": (
                    "MATCH (a:MemoryEntity)-[r:REL]->(b:MemoryEntity) "
                    "WHERE a.scope_id=$sid RETURN count(r) AS c"
                ),
                "parameters": {"sid": SID},
            },
            {
                "statement": (
                    "MATCH (a:MemoryEntity)-[r:REL]->(b:MemoryEntity) "
                    "WHERE a.scope_id=$sid "
                    "RETURN a.name AS a, r.predicate AS p, b.name AS b "
                    "ORDER BY a LIMIT 40"
                ),
                "parameters": {"sid": SID},
            },
            {"statement": "MATCH (n:MemoryEntity) RETURN count(n) AS n"},
            {"statement": "MATCH ()-[r:REL]->() RETURN count(r) AS n"},
        ]
    }
).encode()

req = urllib.request.Request(
    "http://127.0.0.1:7474/db/neo4j/tx/commit",
    data=body,
    headers={"Content-Type": "application/json", "Authorization": "Basic " + auth},
)
with urllib.request.urlopen(req, timeout=30) as resp:
    data = json.load(resp)

labels = ["session_rels", "edges", "total_entities", "total_rels"]
for i, r in enumerate(data.get("results", [])):
    rows = r.get("data", [])
    print(labels[i] if i < len(labels) else f"stmt{i}", "count", len(rows))
    for row in rows[:40]:
        print(" ", row.get("row"))
if data.get("errors"):
    print("errors", data["errors"])
