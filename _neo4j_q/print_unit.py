#!/usr/bin/env python3
import re, sys
from pathlib import Path
import pymysql

uid = sys.argv[1] if len(sys.argv) > 1 else "323adf0e-ea07-4427-ba38-47e712859814"
text = Path(r"E:/configs/sixath/portal/config.yaml").read_text(encoding="utf-8")
m = re.search(r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^\?\s]+)", text)
user, pwd, host, port, db = m.groups()
conn = pymysql.connect(host=host, port=int(port), user=user, password=pwd, database=db, charset="utf8mb4")
cur = conn.cursor()
cur.execute(
    "SELECT id, kind, scope_id, content, metadata FROM memory_units WHERE id=%s",
    (uid,),
)
row = cur.fetchone()
print("id:", row[0])
print("kind:", row[1])
print("scope_id:", row[2])
print("content:", row[3])
print("metadata:", row[4])
conn.close()
