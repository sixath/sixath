import json
import sys
import urllib.request
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

sid = "e9d4c37c-bd34-46df-8233-ab8539e9239b"
headers = {"Authorization": "Bearer dev-bootstrap-token"}
data = None
for base in ["http://10.86.32.78:8000", "http://127.0.0.1:8000"]:
    url = f"{base}/api/v1/sessions/{sid}/messages?limit=200"
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=60) as r:
            data = json.loads(r.read().decode())
        print("OK", base, "n=", len(data.get("items") or []))
        break
    except Exception as e:
        print("FAIL", base, type(e).__name__, e)

if not data:
    raise SystemExit(1)

out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e9d4.json"
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
    print(
        f"[{i}] {m.get('role')} {m.get('createdAt')} clen={len(content)} "
        f"tl={len(tl) if isinstance(tl, list) else 0} tools={dict(tools)}"
    )
    print("  ", c[:280])
    if isinstance(meta, dict):
        for k in ("error", "stream_error", "failed", "task_lock", "skill", "routed_skill"):
            if meta.get(k):
                print("   META", k, str(meta.get(k))[:200])
