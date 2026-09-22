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
sid = "3c02cf70-1012-41c5-8f61-3da4ddf2961f"
cur.execute(
    """
    SELECT id, role, LEFT(content, 200), created_at,
           JSON_TYPE(metadata), JSON_LENGTH(JSON_EXTRACT(metadata, '$.timeline'))
    FROM chat_messages
    WHERE session_id=%s
    ORDER BY created_at
    """,
    (sid,),
)
print("=== messages ===")
for row in cur.fetchall():
    print(row)

cur.execute(
    """
    SELECT id, role, created_at, metadata
    FROM chat_messages
    WHERE session_id=%s AND role='assistant'
    ORDER BY created_at DESC
    LIMIT 3
    """,
    (sid,),
)
print("\n=== assistant timelines ===")
for mid, role, created, meta in cur.fetchall():
    print(f"\n--- {mid} {created} ---")
    if not meta:
        print("no metadata")
        continue
    if isinstance(meta, (bytes, bytearray)):
        meta = meta.decode("utf-8")
    data = json.loads(meta) if isinstance(meta, str) else meta
    tl = data.get("timeline") or []
    print("timeline events", len(tl), "keys", list(data.keys()))
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        kind = ev.get("kind")
        if kind != "tool":
            if kind == "model":
                print(f"  model step={ev.get('step')} {ev.get('phase')}")
            continue
        name = ev.get("toolName") or ev.get("name")
        args = ev.get("arguments") or ev.get("args") or ev.get("input")
        arg_s = json.dumps(args, ensure_ascii=False)[:300] if args else ""
        err = ev.get("error") or ev.get("err") or ""
        result = ev.get("result") or ev.get("output") or ev.get("content") or ""
        if not isinstance(result, str):
            result = json.dumps(result, ensure_ascii=False)
        print(f"  TOOL {name} phase={ev.get('phase')} err={str(err)[:120]!r}")
        if arg_s:
            print(f"    args {arg_s}")
        if result:
            print(f"    result {result[:240].replace(chr(10), ' ')}")
        # dump keys once
        print(f"    ev_keys {list(ev.keys())}")
