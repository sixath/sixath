#!/usr/bin/env python3
"""Live MySQL round-trip for P3-E CommitProceduralRepair result shape.

Simulates what portal auto_commit writes, then verifies fact-only vs procedural
filters via raw SQL (backend process may still be old binary).
"""
import json
import re
import uuid
from datetime import datetime
from pathlib import Path

import pymysql

text = Path(r"E:/configs/sixath/portal/config.yaml").read_text(encoding="utf-8")
m = re.search(r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^\?\s]+)", text)
user, pwd, host, port, db = m.groups()
conn = pymysql.connect(host=host, port=int(port), user=user, password=pwd, database=db, charset="utf8mb4")
cur = conn.cursor()

agent_id = "e8107fb3-e40a-4207-9d9a-6768847aaf79"
session_id = "p3e-live-" + uuid.uuid4().hex[:8]
unit_id = str(uuid.uuid4())
content = "【过程修复 建议】条件 `tool_failed` → 建议 使用 工具序列 [ask_user]"
meta = {
    "kind": "procedural",
    "source": "procedural_repair",
    "procedural_status": "active",
    "failure_code": "tool_failed",
    "support_count": 2,
    "task_family": "zone-4100-agent",
    "binding_action_kind": "tool_sequence",
    "binding_tool_names": ["ask_user"],
    "binding_mode": "suggest",
    "binding_trigger_code": "tool_failed",
    "binding_trigger_query": "ssh",
}
now = datetime.utcnow().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
content_hash = __import__("hashlib").sha256(content.encode()).hexdigest()

cur.execute(
    """
    INSERT INTO memory_units
      (id, scope_type, scope_id, agent_id, content, kind, content_hash, status, source_session_id, metadata, created_at, updated_at)
    VALUES (%s,'session',%s,%s,%s,'procedural',%s,'active',%s,CAST(%s AS JSON), %s, %s)
    """,
    (unit_id, session_id, agent_id, content, content_hash, session_id, json.dumps(meta, ensure_ascii=False), now, now),
)
conn.commit()
print("inserted", unit_id, "session", session_id)

# Fact-like query (portal default): exclude procedural
cur.execute(
    """
    SELECT id FROM memory_units
    WHERE scope_type='session' AND status='active' AND source_session_id=%s
      AND (kind='fact' OR kind='' OR kind IS NULL)
    """,
    (session_id,),
)
facts = cur.fetchall()
print("fact_filter_count", len(facts), "expect 0")

cur.execute(
    """
    SELECT id, kind, LEFT(content,60) FROM memory_units
    WHERE scope_type='session' AND status='active' AND source_session_id=%s AND kind='procedural'
    """,
    (session_id,),
)
procs = cur.fetchall()
print("procedural_filter", procs)

# cleanup
cur.execute("DELETE FROM memory_units WHERE id=%s", (unit_id,))
conn.commit()
print("cleaned", unit_id)

ok = len(facts) == 0 and len(procs) == 1
print("PASS" if ok else "FAIL")
conn.close()
raise SystemExit(0 if ok else 1)
