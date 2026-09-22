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


def dump(sid, label):
    print(f"\n######## {label} {sid}")
    cur.execute(
        "SELECT id, role, created_at, content, metadata FROM chat_messages WHERE session_id=%s AND role='assistant' ORDER BY created_at DESC LIMIT 2",
        (sid,),
    )
    for mid, role, created, content, meta in cur.fetchall():
        if isinstance(meta, (bytes, bytearray)):
            meta = meta.decode("utf-8")
        data = json.loads(meta) if isinstance(meta, str) else (meta or {})
        tl = data.get("timeline") or []
        print(f"\n-- {created} clen={len(content or '')} events={len(tl)}")
        print("content:", (content or "").replace("\n", " | ")[:200])
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            kind = ev.get("kind")
            if kind != "tool":
                print(f"  model step={ev.get('step')} phase={ev.get('phase')}")
                continue
            args = ev.get("arguments")
            arg_s = json.dumps(args, ensure_ascii=False)[:220] if args is not None else ""
            print(f"  TOOL {ev.get('toolName')} step={ev.get('step')} phase={ev.get('phase')} err={ev.get('error')!r}")
            if arg_s:
                print(f"    args {arg_s}")


dump("2fcc2c95-3b00-4f94-a721-8beaaff843fa", "WECOM")
dump("19a34f03-7e18-404e-b734-5edae32c6cb3", "WEB")
