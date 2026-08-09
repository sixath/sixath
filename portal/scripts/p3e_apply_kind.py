#!/usr/bin/env python3
import re
from pathlib import Path
import pymysql

text = Path(r"E:/configs/sixath/portal/config.yaml").read_text(encoding="utf-8")
m = re.search(r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^\?\s]+)", text)
user, pwd, host, port, db = m.groups()
conn = pymysql.connect(host=host, port=int(port), user=user, password=pwd, database=db, charset="utf8mb4")
cur = conn.cursor()

cur.execute("SHOW COLUMNS FROM memory_units LIKE %s", ("kind",))
print("before:", cur.fetchone())

try:
    cur.execute(
        "ALTER TABLE memory_units ADD COLUMN kind VARCHAR(32) NOT NULL DEFAULT 'fact' AFTER content"
    )
    print("added_column")
except Exception as e:
    print("add_column:", type(e).__name__, e)

try:
    cur.execute("ALTER TABLE memory_units ADD INDEX idx_mu_kind (scope_type, kind, status)")
    print("added_index")
except Exception as e:
    print("add_index:", type(e).__name__, e)

conn.commit()
cur.execute("SHOW COLUMNS FROM memory_units LIKE %s", ("kind",))
print("after:", cur.fetchone())
cur.execute(
    "SELECT id, name FROM agents WHERE name=%s LIMIT 1",
    ("zone-4100-agent",),
)
print("pilot_agent:", cur.fetchone())
conn.close()
