import json
import sys
from collections import Counter

import pymysql

sys.stdout.reconfigure(encoding="utf-8")

SID = "2fcc2c95-3b00-4f94-a721-8beaaff843fa"

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
    "SELECT id, agent_id, user_id, title, created_at, updated_at FROM chat_sessions WHERE id=%s",
    (SID,),
)
print("SESSION", cur.fetchone())

cur.execute(
    "SELECT id, role, content, metadata, created_at, active FROM chat_messages WHERE session_id=%s ORDER BY created_at",
    (SID,),
)
rows = cur.fetchall()
print("N_MESSAGES", len(rows))

out = []
for mid, role, content, meta, created, active in rows:
    if isinstance(meta, (bytes, bytearray)):
        meta = meta.decode("utf-8")
    data = json.loads(meta) if isinstance(meta, str) else (meta or {})
    tl = data.get("timeline") if isinstance(data, dict) else None
    rec = {
        "id": mid,
        "role": role,
        "created": str(created),
        "active": active,
        "content": content,
        "meta_keys": list(data.keys()) if isinstance(data, dict) else None,
        "timeline_len": len(tl) if isinstance(tl, list) else None,
    }
    tools = []
    kinds = Counter()
    if isinstance(tl, list):
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            kind = ev.get("kind") or ev.get("type") or "?"
            kinds[kind] += 1
            brief = {
                "kind": kind,
                "step": ev.get("step"),
                "phase": ev.get("phase"),
                "toolName": ev.get("toolName"),
                "error": ev.get("error"),
                "status": ev.get("status"),
                "durationMs": ev.get("durationMs") or ev.get("duration_ms"),
                "keys": list(ev.keys()),
            }
            args = ev.get("arguments") or ev.get("args") or ev.get("input")
            if args is not None:
                brief["arguments"] = args
            result = ev.get("result") or ev.get("output") or ev.get("content")
            if result is not None:
                if not isinstance(result, str):
                    result = json.dumps(result, ensure_ascii=False)
                brief["result_head"] = result[:800]
                brief["result_len"] = len(result)
            if kind in ("model", "tool", "error", "policy", "retry", "gate", "finish"):
                # keep extra interesting fields
                for k in (
                    "prompt",
                    "reason",
                    "decision",
                    "retryPrompt",
                    "finishReason",
                    "used",
                    "injected",
                    "family",
                    "dropped",
                    "nudge",
                    "goalDrift",
                    "text",
                    "contentText",
                ):
                    if k in ev:
                        v = ev[k]
                        if isinstance(v, str) and len(v) > 400:
                            v = v[:400] + "...(trunc)"
                        brief[k] = v
            tools.append(brief)
    rec["kinds"] = dict(kinds)
    rec["timeline"] = tools
    rec["other_meta"] = {
        k: data[k]
        for k in data
        if k != "timeline" and k in data
    }
    # shrink huge other_meta
    other = rec["other_meta"]
    for k, v in list(other.items()):
        s = json.dumps(v, ensure_ascii=False) if not isinstance(v, str) else v
        if len(s) > 1500:
            other[k] = s[:1500] + "...(trunc)"
    out.append(rec)
    print("\n====", role, mid, created, "active", active, "clen", len(content or ""))
    print("content:", (content or "")[:500].replace("\n", " | "))
    print("kinds", dict(kinds), "meta_keys", rec["meta_keys"])
    for t in tools:
        print(" ", {k: t[k] for k in t if k not in ("keys", "result_head") or t.get("kind") != "model"})

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_aff843fa.json"
with open(path, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, indent=2)
print("\nwrote", path)
