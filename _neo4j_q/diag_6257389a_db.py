import pymysql

conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
sid = "6257389a-4712-4e17-9977-dd8f161592a8"
cur = conn.cursor()

print("=== session ===")
cur.execute(
    "SELECT id, agent_id, title, rewind_count, readonly, created_at, updated_at FROM chat_sessions WHERE id=%s",
    (sid,),
)
print(cur.fetchone())

print("\n=== messages ===")
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

print("\n=== turn_traces ===")
try:
    cur.execute(
        """
        SELECT request_id, active, created_at, CHAR_LENGTH(CAST(payload AS CHAR))
        FROM turn_traces
        WHERE session_id=%s
        ORDER BY created_at
        """,
        (sid,),
    )
    rows = cur.fetchall()
    print("n", len(rows))
    for row in rows:
        print(row)
except Exception as e:
    print("turn_traces err", e)

print("\n=== show tables like %trace% / %run% / %job% ===")
cur.execute("SHOW TABLES")
for (t,) in cur.fetchall():
    if any(x in t.lower() for x in ("trace", "run", "job", "event", "stream", "cancel")):
        print(t)

conn.close()
