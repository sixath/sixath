#!/usr/bin/env python3
import re, sys
from pathlib import Path
import pymysql

sid = sys.argv[1] if len(sys.argv) > 1 else "9cc73ef0-e288-469e-a264-92b7f8d17f41"
text = Path(r"E:/configs/sixath/portal/config.yaml").read_text(encoding="utf-8")
m = re.search(r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^\?\s]+)", text)
user, pwd, host, port, db = m.groups()
conn = pymysql.connect(host=host, port=int(port), user=user, password=pwd, database=db, charset="utf8mb4")
cur = conn.cursor()
cur.execute(
    """
    SELECT id, kind, scope_type, scope_id, LEFT(content,160), created_at
    FROM memory_units
    WHERE scope_id=%s OR JSON_UNQUOTE(JSON_EXTRACT(metadata,'$.source_session_id'))=%s
    ORDER BY created_at DESC LIMIT 20
    """,
    (sid, sid),
)
rows = cur.fetchall()
print("session_units:", rows)
cur.execute(
    """
    SELECT id, kind, scope_id, LEFT(content,120), created_at
    FROM memory_units
    WHERE kind='procedural'
    ORDER BY created_at DESC LIMIT 10
    """
)
print("recent_procedural:", cur.fetchall())
conn.close()
