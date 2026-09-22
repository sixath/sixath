import json
import sys
import urllib.request
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

sid = "19a34f03-7e18-404e-b734-5edae32c6cb3"
headers = {"Authorization": "Bearer dev-bootstrap-token"}
data = None
for base in ["http://10.86.32.78:8000", "http://127.0.0.1:8000", "http://10.86.32.78:5173"]:
    url = f"{base}/api/v1/sessions/{sid}/messages?limit=200"
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=30) as r:
            data = json.loads(r.read().decode())
        print("OK", base, "n=", len(data.get("items") or []))
        break
    except Exception as e:
        print("FAIL", base, type(e).__name__, e)

if not data:
    raise SystemExit(1)

out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_19a34f.json"
with open(out_path, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)
print("wrote", out_path)

items = data.get("items") or []
print("total messages", len(items))
for i, m in enumerate(items):
    content = m.get("content") or ""
    c = content.replace("\n", " | ")
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") if isinstance(meta, dict) else []
    tools = Counter()
    if isinstance(tl, list):
        for ev in tl:
            if isinstance(ev, dict) and ev.get("kind") == "tool":
                tools[ev.get("toolName") or "?"] += 1
    role = m.get("role")
    created = m.get("createdAt")
    mid = m.get("id")
    tl_len = len(tl) if isinstance(tl, list) else 0
    print(f"[{i}] {role} {created} clen={len(content)} tl={tl_len} tools={dict(tools)} id={mid}")
    print("  ", c[:800])
    if isinstance(meta, dict):
        keys = list(meta.keys())
        print("   meta_keys", keys)
        for k in (
            "error",
            "stream_error",
            "failed",
            "task_lock",
            "skill",
            "routed_skill",
            "empty_hit",
            "speak_gate",
            "hit",
            "evidence_incomplete",
            "reason",
            "status",
            "intent",
            "resolved_intent",
            "turn_intent",
            "grounding",
            "http_grounding",
        ):
            if meta.get(k):
                print("   META", k, str(meta.get(k))[:400])
        if isinstance(tl, list) and tl:
            kinds = Counter()
            for ev in tl:
                if not isinstance(ev, dict):
                    continue
                k = ev.get("kind") or ev.get("type") or "?"
                kinds[k] += 1
                if ev.get("kind") == "tool":
                    args = ev.get("arguments")
                    arg_s = json.dumps(args, ensure_ascii=False)[:400] if args is not None else ""
                    print(f"   TOOL {ev.get('toolName')} step={ev.get('step')} phase={ev.get('phase')} err={ev.get('error')!r}")
                    if arg_s:
                        print(f"     args {arg_s}")
                    result = ev.get("result") or ev.get("output") or ev.get("content") or ""
                    if not isinstance(result, str):
                        result = json.dumps(result, ensure_ascii=False)
                    if result:
                        print(f"     result {result[:300].replace(chr(10), ' ')}")
            print("   timeline_kinds", dict(kinds))
