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

# Find recent messages mentioning the commit or wecom wrap of this question
cur.execute(
    """
    SELECT m.session_id, m.id, m.role, m.created_at, LEFT(m.content, 180), s.title, s.agent_id
    FROM chat_messages m
    LEFT JOIN chat_sessions s ON s.id = m.session_id
    WHERE m.content LIKE %s OR m.content LIKE %s
    ORDER BY m.created_at DESC
    LIMIT 20
    """,
    ("%3739d7d%", "%diff commit%"),
)
print("=== matching messages ===")
for row in cur.fetchall():
    print(row[0], row[1], row[2], row[3], repr(row[4]), row[5], row[6])

# also search timeline metadata
cur.execute(
    """
    SELECT session_id, id, role, created_at, LEFT(content, 120)
    FROM chat_messages
    WHERE metadata LIKE %s
    ORDER BY created_at DESC
    LIMIT 10
    """,
    ("%3739d7d%",),
)
print("\n=== metadata matches ===")
for row in cur.fetchall():
    print(row)
