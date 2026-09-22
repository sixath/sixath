#!/usr/bin/env python3
import json
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
cur.execute(
    "SELECT content, metadata FROM chat_messages WHERE session_id=%s AND role='assistant'",
    (sid,),
)
content, meta = cur.fetchone()
if isinstance(meta, (bytes, bytearray)):
    meta = meta.decode("utf-8")
data = json.loads(meta) if isinstance(meta, str) else meta
tl = data.get("timeline") or []

print("=== assistant content mentions of counts ===")
for needle in ["468", "370", "条", "hits", "total"]:
    print(needle, content.count(needle) if isinstance(content, str) else 0)

print("=== content tail ===")
print(content[-2500:] if isinstance(content, str) else content)

print("\n=== es_log_query / result_stats / run_result_script summaries ===")
for ev in tl:
    if ev.get("kind") != "tool":
        continue
    name = ev.get("toolName")
    result = ev.get("result")
    args = ev.get("arguments")
    truncated = ev.get("truncated")
    rlen = 0
    preview = ""
    if isinstance(result, str):
        rlen = len(result)
        preview = result[:400].replace("\n", " ")
    elif result is not None:
        s = json.dumps(result, ensure_ascii=False)
        rlen = len(s)
        preview = s[:400]
    arg_s = json.dumps(args, ensure_ascii=False)[:200] if args is not None else ""
    hits = None
    if isinstance(result, str):
        for key in ("total", "hits", "returned", "count", "unique"):
            if key in result[:800] or f'"{key}"' in result[:2000]:
                pass
        # parse json if possible
        try:
            parsed = json.loads(result)
            if isinstance(parsed, dict):
                hits = {k: parsed.get(k) for k in ("total", "took", "hits", "returned", "count", "unique_count", "row_count", "truncated", "page", "size") if k in parsed}
                if "hits" in parsed and isinstance(parsed["hits"], dict):
                    hits["hits.total"] = parsed["hits"].get("total")
                    hhits = parsed["hits"].get("hits")
                    if isinstance(hhits, list):
                        hits["hits.hits_len"] = len(hhits)
        except Exception:
            pass
    print(f"step={ev.get('step')} tool={name} truncated={truncated} result_len={rlen} hits={hits}")
    print("  args", arg_s)
    print("  preview", preview[:240])

conn.close()
