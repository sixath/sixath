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

# second assistant content
cur.execute(
    "SELECT content FROM chat_messages WHERE id=%s",
    ("17c2916f-9482-4ab5-8041-901b45a9e926",),
)
row = cur.fetchone()
text = row[0] if row else ""
print("second asst len", len(text))
print(text[:1500])
print("---TAIL---")
print(text[-800:])

# rewind
cur.execute(
    "SELECT rewind_count, readonly FROM chat_sessions WHERE id=%s",
    ("75f7b4da-a700-4b27-8be9-787aa1c895d5",),
)
print("session", cur.fetchone())

conn.close()
