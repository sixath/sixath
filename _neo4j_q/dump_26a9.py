import json
import sys
import urllib.request
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

sid = "26a9c0ad-ed81-4a14-bbb8-e30076f89572"
headers = {"Authorization": "Bearer dev-bootstrap-token"}
data = None
for base in ["http://10.86.32.78:8000", "http://127.0.0.1:8000"]:
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

out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_26a9.json"
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
    errors = []
    if isinstance(tl, list):
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            if ev.get("kind") == "tool":
                tools[ev.get("toolName") or "?"] += 1
                if ev.get("error") or ev.get("phase") == "failed":
                    errors.append({
                        "tool": ev.get("toolName"),
                        "error": str(ev.get("error") or "")[:200],
                        "phase": ev.get("phase"),
                    })
    role = m.get("role")
    print(f"[{i}] {role} {m.get('createdAt')} clen={len(content)} tl={len(tl) if isinstance(tl, list) else 0} tools={dict(tools)} id={m.get('id')}")
    print("  ", c[:800])
    if errors:
        print("   ERRORS", errors[:5])
    if isinstance(meta, dict):
        for k in ("error", "stream_error", "failed", "reason", "status"):
            if meta.get(k):
                print("   META", k, str(meta.get(k))[:400])
