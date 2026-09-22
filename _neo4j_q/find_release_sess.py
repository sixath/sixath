import json
import pymysql

conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()

print("=== sessions ending aff843fa ===")
cur.execute(
    "SELECT id, agent_id, user_id, title, updated_at FROM chat_sessions WHERE id LIKE %s ORDER BY updated_at DESC LIMIT 10",
    ("%aff843fa%",),
)
for row in cur.fetchall():
    print(row)

print("\n=== sessions for user + ops-agent ===")
cur.execute(
    "SELECT id, title, updated_at FROM chat_sessions WHERE user_id=%s AND agent_id=%s ORDER BY updated_at DESC LIMIT 15",
    ("woudFdDgaANOiR6h1juihhoLmJ2HZ4mQ", "b880051a-a7de-4d91-afea-2ad41269191c"),
)
for row in cur.fetchall():
    print(row)

print("\n=== messages with batchReleaseInstance ===")
cur.execute(
    "SELECT m.id, m.session_id, m.role, LEFT(m.content, 300), m.created_at FROM chat_messages m WHERE m.content LIKE %s ORDER BY m.created_at DESC LIMIT 10",
    ("%batchReleaseInstance%",),
)
for row in cur.fetchall():
    print(row)

print("\n=== messages with trace_id bb110c91 ===")
cur.execute(
    "SELECT m.id, m.session_id, m.role, LEFT(m.content, 300), m.created_at FROM chat_messages m WHERE m.content LIKE %s ORDER BY m.created_at DESC LIMIT 10",
    ("%bb110c9194abc73fa8471092d989d5f7%",),
)
for row in cur.fetchall():
    print(row)
