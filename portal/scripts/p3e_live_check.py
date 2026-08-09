#!/usr/bin/env python3
"""Live checks for P3-E: kind column, zone-4100 agent, optional procedural units."""
import re
import sys
from pathlib import Path

try:
    import pymysql
except ImportError:
    print("FAIL: pymysql missing")
    sys.exit(1)

text = Path(r"E:/configs/sixath/portal/config.yaml").read_text(encoding="utf-8")
m = re.search(r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^\?\s]+)", text)
if not m:
    print("FAIL: cannot parse MySQL DSN")
    sys.exit(1)
user, pwd, host, port, db = m.groups()
conn = pymysql.connect(host=host, port=int(port), user=user, password=pwd, database=db, charset="utf8mb4")
cur = conn.cursor()

cur.execute("SHOW COLUMNS FROM memory_units LIKE 'kind'")
kind_col = cur.fetchone()
print("kind_column:", kind_col)

if kind_col is None:
    print("APPLYING migration 011...")
    sql = Path(r"d:/workspace/github/sixath/portal/migrations/011_memory_units_kind.sql").read_text(encoding="utf-8")
    for stmt in sql.split(";"):
        stmt = stmt.strip()
        if stmt and not stmt.startswith("--"):
            cur.execute(stmt)
    conn.commit()
    cur.execute("SHOW COLUMNS FROM memory_units LIKE 'kind'")
    print("kind_column_after:", cur.fetchone())

cur.execute(
    "SELECT id, name FROM agents WHERE name LIKE %s OR name LIKE %s OR id LIKE %s LIMIT 10",
    ("%zone-4100%", "%4100%", "%4100%"),
)
agents = cur.fetchall()
print("zone_agents:", agents)

cur.execute(
    "SELECT id, kind, LEFT(content,80), JSON_EXTRACT(metadata,'$.kind') FROM memory_units WHERE kind='procedural' OR JSON_EXTRACT(metadata,'$.kind')='procedural' ORDER BY updated_at DESC LIMIT 10"
)
print("procedural_units:", cur.fetchall())

cur.execute("SELECT COUNT(*) FROM memory_units WHERE kind='fact' OR kind='' OR kind IS NULL")
print("fact_like_count:", cur.fetchone()[0])

conn.close()
print("OK")
