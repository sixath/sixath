import json
import sys

import pymysql

sys.stdout.reconfigure(encoding="utf-8")

conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()

cur.execute(
    "SELECT payload_json FROM turn_traces WHERE id=%s",
    ("4bc610dc-8e65-440b-82ea-a883839714ff",),
)
(payload,) = cur.fetchone()
data = json.loads(payload)
print("keys", list(data.keys()))
print("ncalls", len(data.get("calls") or []))
for c in data.get("calls") or []:
    print(
        f"step={c.get('step')} tool={c.get('tool_name')} err={c.get('error')!r} "
        f"args={json.dumps(c.get('arguments') or {}, ensure_ascii=False)[:180]}"
    )
    prev = (c.get("result_preview") or "")[:120].replace("\n", " ")
    if prev:
        print("  preview", prev)

print("\n=== agent_tools for ops-agent ===")
cur.execute("SHOW COLUMNS FROM agent_tools")
print(cur.fetchall())
cur.execute("SELECT * FROM agent_tools WHERE agent_id=%s LIMIT 50", ("b880051a-a7de-4d91-afea-2ad41269191c",))
rows = cur.fetchall()
print("nrows", len(rows))
for r in rows[:40]:
    print(r)

print("\n=== agent_mcp_servers ===")
cur.execute("SHOW COLUMNS FROM agent_mcp_servers")
print(cur.fetchall())
cur.execute(
    "SELECT * FROM agent_mcp_servers WHERE agent_id=%s",
    ("b880051a-a7de-4d91-afea-2ad41269191c",),
)
for r in cur.fetchall():
    print(r)

print("\n=== agents row tools/config snippet ===")
cur.execute("SHOW COLUMNS FROM agents")
cols = [c[0] for c in cur.fetchall()]
print(cols)
cur.execute("SELECT * FROM agents WHERE id=%s", ("b880051a-a7de-4d91-afea-2ad41269191c",))
row = cur.fetchone()
if row:
    rec = dict(zip(cols, row))
    for k in rec:
        v = rec[k]
        if k.lower() in ("system_prompt", "config", "tools", "workspace"):
            s = str(v)
            print(k, s[:300])
        elif k in ("name", "id", "max_steps"):
            print(k, v)
