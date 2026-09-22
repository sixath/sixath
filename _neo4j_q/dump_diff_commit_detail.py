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

sid = "2fcc2c95-3b00-4f94-a721-8beaaff843fa"
mid = "7b8a63f5-2bd6-4e4a-a360-b9695596b6cd"

cur.execute(
    "SELECT content, metadata FROM chat_messages WHERE id=%s",
    (mid,),
)
content, meta = cur.fetchone()
if isinstance(meta, (bytes, bytearray)):
    meta = meta.decode("utf-8")
data = json.loads(meta) if isinstance(meta, str) else (meta or {})

print("===== FULL CONTENT =====")
print(content)
print("\n===== CONTENT LEN =====", len(content or ""))

tl = data.get("timeline") or []
print("\n===== TIMELINE events", len(tl), "keys", list(data.keys())[:30])
for ev in tl:
    if not isinstance(ev, dict):
        continue
    kind = ev.get("kind")
    if kind != "tool":
        phase = ev.get("phase")
        step = ev.get("step")
        txt = (ev.get("text") or ev.get("content") or "")[:120]
        print(f"  {kind} step={step} phase={phase} {txt!r}")
        continue
    args = ev.get("arguments")
    arg_s = json.dumps(args, ensure_ascii=False)[:300] if args is not None else ""
    err = ev.get("error")
    out = ev.get("output") or ev.get("result") or ev.get("content") or ""
    if isinstance(out, (dict, list)):
        out_s = json.dumps(out, ensure_ascii=False)[:200]
    else:
        out_s = str(out)[:200]
    print(f"  TOOL {ev.get('toolName')} step={ev.get('step')} phase={ev.get('phase')} err={err!r}")
    if arg_s:
        print(f"    args {arg_s}")
    if out_s:
        print(f"    out {out_s}")

# session turn list
print("\n===== SESSION TURNS =====")
cur.execute(
    """
    SELECT id, role, created_at, LEFT(content, 160)
    FROM chat_messages
    WHERE session_id=%s
    ORDER BY created_at ASC
    """,
    (sid,),
)
for row in cur.fetchall():
    print(row[1], row[2], repr(row[3]))
