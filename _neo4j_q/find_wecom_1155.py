import json
import sys
from collections import Counter

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

print("=== recent ops-agent sessions ===")
cur.execute(
    """
    SELECT id, user_id, title, updated_at
    FROM chat_sessions
    WHERE agent_id=%s
    ORDER BY updated_at DESC
    LIMIT 8
    """,
    ("b880051a-a7de-4d91-afea-2ad41269191c",),
)
for row in cur.fetchall():
    print(row)

print("\n=== messages around 11:55 with 释放 ===")
cur.execute(
    """
    SELECT m.id, m.session_id, m.role, LEFT(m.content, 180), m.created_at
    FROM chat_messages m
    WHERE m.created_at >= '2026-08-25 11:50:00'
      AND m.content LIKE %s
    ORDER BY m.created_at
    """,
    ("%bb110c9194abc73fa8471092d989d5f7%",),
)
for row in cur.fetchall():
    print(row)
