# -*- coding: utf-8 -*-
import json
import pymysql

sid = "6257389a-4712-4e17-9977-dd8f161592a8"
conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()

print("=== turn_traces columns ===")
cur.execute("SHOW COLUMNS FROM turn_traces")
for row in cur.fetchall():
    print(row)

print("\n=== turn_traces rows ===")
cur.execute("SELECT * FROM turn_traces WHERE session_id=%s ORDER BY created_at", (sid,))
cols = [d[0] for d in cur.description]
print("cols", cols)
rows = cur.fetchall()
print("n", len(rows))
for row in rows:
    d = dict(zip(cols, row))
    # truncate large fields
    for k, v in list(d.items()):
        if isinstance(v, (bytes, bytearray)):
            d[k] = f"<bytes {len(v)}>"
        elif isinstance(v, str) and len(v) > 500:
            d[k] = v[:500] + f"...<{len(v)}>"
    print(json.dumps(d, ensure_ascii=False, default=str))

# Also look at assistant message metadata for interrupted ones
focus_ids = [
    "4373adca-552c-4492-bdf6-5c936ede518b",  # msg11 short
    "31e0d9b7-cb08-4479-b131-75eec292be97",  # msg13 empty
    "23e0766c-713e-48d3-8440-973fec5b1f5e",  # msg17 short 23
    "9123de61-b340-4cfe-909d-45646b2582be",  # msg21 stopped before update
    "4df45644-e912-44f3-9f84-58feeef4ec6c",  # msg23 short 19
]
print("\n=== focus message meta (no timeline dump) ===")
for mid in focus_ids:
    cur.execute(
        "SELECT id, role, CHAR_LENGTH(content), created_at, metadata FROM chat_messages WHERE id=%s",
        (mid,),
    )
    row = cur.fetchone()
    if not row:
        print("missing", mid)
        continue
    mid, role, clen, created, meta = row
    print("---", mid, role, clen, created)
    if meta:
        try:
            obj = json.loads(meta) if isinstance(meta, str) else meta
        except Exception:
            obj = meta
        if isinstance(obj, dict):
            tl = obj.pop("timeline", None)
            print("meta keys", list(obj.keys()))
            print(json.dumps(obj, ensure_ascii=False, default=str)[:2000])
            if isinstance(tl, list):
                print("timeline n", len(tl))
                for ev in tl:
                    print(" ", json.dumps(ev, ensure_ascii=False)[:500])
        else:
            print(str(meta)[:500])

conn.close()
