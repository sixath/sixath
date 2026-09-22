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

sid = "2fcc2c95-3b00-4f94-a721-8beaaff843fa"
aid = "b880051a-a7de-4d91-afea-2ad41269191c"

print("=== turn_traces columns ===")
cur.execute("SHOW COLUMNS FROM turn_traces")
for row in cur.fetchall():
    print(row)

print("\n=== recent turn_traces for session ===")
cur.execute(
    """
    SELECT id, request_id, turn_seq, created_at, LEFT(payload_json, 400)
    FROM turn_traces
    WHERE session_id=%s
    ORDER BY created_at DESC
    LIMIT 8
    """,
    (sid,),
)
rows = cur.fetchall()
print("count", len(rows))
for r in rows:
    print("---", r[0], r[1], r[2], r[3])
    print(r[4][:400] if r[4] else None)

print("\n=== agent tools ===")
cur.execute(
    "SHOW TABLES LIKE '%agent%tool%'"
)
print(cur.fetchall())
cur.execute("SHOW TABLES")
tables = [t[0] for t in cur.fetchall()]
print("tables with tool/skill/bind", [t for t in tables if any(x in t.lower() for x in ("tool", "skill", "bind", "mcp"))])
