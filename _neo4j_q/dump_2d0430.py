import json
import sys
import urllib.request
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

sid = "2d0430f0-2369-43a3-9d64-b17a616159b7"
aid = "1583f45b-d5c3-41ab-b1f7-1aa5539126d4"
headers = {"Authorization": "Bearer dev-bootstrap-token"}
data = None
used_base = None
for base in ["http://10.86.32.78:8000", "http://127.0.0.1:8000", "http://10.86.32.78:5173"]:
    url = f"{base}/api/v1/sessions/{sid}/messages?limit=200"
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=30) as r:
            data = json.loads(r.read().decode())
        print("OK", base, "n=", len(data.get("items") or []))
        used_base = base
        break
    except Exception as e:
        print("FAIL", base, type(e).__name__, e)

if not data:
    raise SystemExit(1)

out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_2d0430.json"
with open(out_path, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)
print("wrote", out_path)

# also fetch session + agent
for path, label in [
    (f"/api/v1/sessions/{sid}", "session"),
    (f"/api/v1/agents/{aid}", "agent"),
]:
    url = f"{used_base}{path}"
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=15) as r:
            obj = json.loads(r.read().decode())
        print("===", label, "===")
        print(json.dumps(obj, ensure_ascii=False)[:3000])
    except Exception as e:
        print("FAIL", label, e)

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
    print("  ", c[:1200])
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
            "post_model",
            "policy",
            "retry",
        ):
            if meta.get(k):
                print("   META", k, str(meta.get(k))[:800])
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
                elif k in ("thinking", "reasoning", "thought", "model"):
                    txt = ev.get("content") or ev.get("text") or ev.get("thought") or ""
                    if txt:
                        print(f"   {k} {str(txt)[:400].replace(chr(10), ' ')}")
            print("   timeline_kinds", dict(kinds))
            # dump unique kinds sample
            for ev in tl[:30]:
                if isinstance(ev, dict):
                    print("   EV", {kk: str(ev.get(kk))[:120] for kk in list(ev.keys())[:12]})
