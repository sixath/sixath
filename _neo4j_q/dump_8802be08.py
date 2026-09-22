import json
import sys
import urllib.request
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

sid = "8802be08-db85-49bd-9498-a6cc2f1c1bb0"
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

out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8802be08.json"
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
    last_model = None
    if isinstance(tl, list):
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            name = ev.get("toolName") or ev.get("name") or ""
            if isinstance(ev.get("tool"), dict):
                name = ev["tool"].get("name") or name
            kind = ev.get("kind") or ev.get("type") or ev.get("event") or "?"
            if kind == "tool" or name:
                tools[name or kind] += 1
            if kind == "model":
                last_model = ev
    print(
        f"[{i}] {m.get('role')} {m.get('createdAt')} clen={len(content)} "
        f"tl={len(tl) if isinstance(tl, list) else 0} tools={dict(tools)}"
    )
    print("  ", c[:350])
    if isinstance(meta, dict):
        print("   interrupted", meta.get("interrupted"))
        print("   meta_keys", list(meta.keys())[:40])
        if last_model:
            print("   last_model", {k: last_model.get(k) for k in ("kind", "phase", "step", "seq", "mode", "model")})
