import pymysql
import json

conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
sid = "75f7b4da-a700-4b27-8be9-787aa1c895d5"
cur = conn.cursor()

print("=== session ===")
cur.execute(
    "SELECT id, title, rewind_count, readonly, created_at, updated_at FROM chat_sessions WHERE id=%s",
    (sid,),
)
print(cur.fetchone())

print("\n=== all messages (incl inactive) ===")
cur.execute(
    """
    SELECT id, role, active, created_at,
           CHAR_LENGTH(content) AS content_len,
           LENGTH(metadata) AS meta_bytes
    FROM chat_messages
    WHERE session_id=%s
    ORDER BY created_at
    """,
    (sid,),
)
for row in cur.fetchall():
    print(row)

print("\n=== turn traces ===")
cur.execute(
    """
    SELECT request_id, active, created_at, CHAR_LENGTH(CAST(payload AS CHAR))
    FROM turn_traces
    WHERE session_id=%s
    ORDER BY created_at
    LIMIT 20
    """,
    (sid,),
)
try:
    rows = cur.fetchall()
    print("n", len(rows))
    for row in rows:
        print(row)
except Exception as e:
    print("turn_traces err", e)
    cur.execute("SHOW TABLES LIKE '%trace%'")
    print("tables", cur.fetchall())

conn.close()
