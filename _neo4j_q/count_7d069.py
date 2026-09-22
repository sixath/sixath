#!/usr/bin/env python3
import json
import urllib.request

import pymysql

sid = "7d069a05-0699-42ea-a3af-2407e0a1aa20"
conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()
print("=== chat_messages by active, role ===")
cur.execute(
    "SELECT active, role, COUNT(*) FROM chat_messages WHERE session_id=%s GROUP BY active, role ORDER BY active, role",
    (sid,),
)
for r in cur.fetchall():
    print(r)
cur.execute("SELECT COUNT(*) FROM chat_messages WHERE session_id=%s", (sid,))
print("total", cur.fetchone()[0])
cur.execute("SELECT COUNT(*) FROM chat_messages WHERE session_id=%s AND active=1", (sid,))
print("active", cur.fetchone()[0])

print("=== relevant tables ===")
cur.execute("SHOW TABLES")
tables = [t[0] for t in cur.fetchall()]
for t in tables:
    if any(x in t.lower() for x in ["unit", "trace", "message", "session"]):
        print("table", t)

for t in tables:
    if "unit" in t.lower() or "trace" in t.lower():
        try:
            cur.execute(f"SELECT COUNT(*) FROM `{t}` WHERE session_id=%s", (sid,))
            print("count", t, cur.fetchone()[0])
        except Exception as e:
            print("skip", t, e)

print("=== first/last 3 active messages ===")
cur.execute(
    """
    SELECT id, role, CHAR_LENGTH(content), created_at
    FROM chat_messages
    WHERE session_id=%s AND active=1
    ORDER BY created_at ASC
    LIMIT 3
    """,
    (sid,),
)
print("first", cur.fetchall())
cur.execute(
    """
    SELECT id, role, CHAR_LENGTH(content), created_at
    FROM chat_messages
    WHERE session_id=%s AND active=1
    ORDER BY created_at DESC
    LIMIT 3
    """,
    (sid,),
)
print("last", cur.fetchall())

print("=== ASC LIMIT 100 last row ===")
cur.execute(
    """
    SELECT id, role, created_at FROM (
      SELECT id, role, created_at
      FROM chat_messages
      WHERE session_id=%s AND active=1
      ORDER BY created_at ASC
      LIMIT 100
    ) t ORDER BY created_at DESC LIMIT 1
    """,
    (sid,),
)
print(cur.fetchone())

print("=== DESC LIMIT 100 first (newest of window) ===")
cur.execute(
    """
    SELECT id, role, created_at
    FROM chat_messages
    WHERE session_id=%s AND active=1
    ORDER BY created_at DESC
    LIMIT 1
    """,
    (sid,),
)
print("newest", cur.fetchone())

conn.close()

for url in [
    f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages",
    f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages?limit=100",
    f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages?limit=1000",
    f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages",
]:
    req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            data = json.loads(resp.read().decode())
            items = data.get("items") or []
            print("API", url, "items", len(items))
            if items:
                print("  first", items[0].get("id"), items[0].get("role"), items[0].get("created_at"))
                print("  last", items[-1].get("id"), items[-1].get("role"), items[-1].get("created_at"))
    except Exception as e:
        print("API fail", url, e)
