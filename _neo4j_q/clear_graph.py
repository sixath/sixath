#!/usr/bin/env python3
"""Clear Sixath MemoryEntity / REL data in Neo4j. Password from live agent_extra.yaml."""
import base64
import json
import re
import urllib.request
from pathlib import Path

CONF = Path(r"E:/configs/sixath/portal/agent_extra.yaml")
text = CONF.read_text(encoding="utf-8")
m = re.search(r'password:\s*"([^"]+)"', text)
if not m:
    raise SystemExit("password not found")
passw = m.group(1)
auth = base64.b64encode(("neo4j:" + passw).encode()).decode()

stmts = [
    {"statement": "MATCH ()-[r:REL]->() RETURN count(r) AS n"},
    {"statement": "MATCH (n:MemoryEntity) RETURN count(n) AS n"},
    {"statement": "MATCH ()-[r:REL]->() DELETE r RETURN count(*) AS deleted_rels"},
    {"statement": "MATCH (n:MemoryEntity) DETACH DELETE n RETURN count(*) AS deleted_ents"},
    {"statement": "MATCH ()-[r:REL]->() RETURN count(r) AS n"},
    {"statement": "MATCH (n:MemoryEntity) RETURN count(n) AS n"},
]

body = json.dumps({"statements": stmts}).encode()
req = urllib.request.Request(
    "http://127.0.0.1:7474/db/neo4j/tx/commit",
    data=body,
    headers={"Content-Type": "application/json", "Authorization": "Basic " + auth},
)
with urllib.request.urlopen(req, timeout=60) as resp:
    data = json.load(resp)

labels = [
    "before_rels",
    "before_ents",
    "deleted_rels",
    "deleted_ents",
    "after_rels",
    "after_ents",
]
if data.get("errors"):
    print("errors", data["errors"])
    raise SystemExit(1)
for i, r in enumerate(data.get("results", [])):
    rows = [row.get("row") for row in r.get("data", [])]
    print(labels[i] if i < len(labels) else f"stmt{i}", rows)
