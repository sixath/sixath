import json
import urllib.request
from collections import Counter

sid = "37ceddfc-3244-4a6b-b7c0-b440f7769b3e"
url = f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages?limit=200"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
data = json.loads(urllib.request.urlopen(req, timeout=30).read().decode())
items = data.get("items") or []
print("total", len(items))
for i, m in enumerate(items):
    content = (m.get("content") or "").replace("\n", " | ")
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") if isinstance(meta, dict) else []
    tools = Counter()
    errors = []
    phases = Counter()
    if isinstance(tl, list):
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            kind = ev.get("kind") or "?"
            phases[f"{kind}:{ev.get('phase')}"] += 1
            if ev.get("kind") == "tool":
                tools[ev.get("toolName") or "?"] += 1
            err = ev.get("error")
            if err:
                errors.append((ev.get("kind"), ev.get("toolName"), str(err)[:300]))
    print(
        f"[{i}] {m.get('role')} {m.get('createdAt')} clen={len(m.get('content') or '')} "
        f"tl={len(tl) if isinstance(tl, list) else 0} tools={dict(tools)}"
    )
    print("   content:", content[:400])
    if isinstance(meta, dict):
        for k, v in meta.items():
            if k == "timeline":
                continue
            s = str(v)
            if s and s not in ("None", "[]", "{}", "0", "false"):
                print("   META", k, s[:400])
    if errors:
        print("   ERRORS", errors[:12])
    if isinstance(tl, list) and tl:
        print("   phases", dict(phases))
        last = []
        for ev in tl[-8:]:
            last.append(
                {
                    k: ev.get(k)
                    for k in ("kind", "toolName", "phase", "status", "error", "step", "model")
                    if k in ev
                }
            )
        print("   last_ev", last)

out = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_37ced.json"
with open(out, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)
print("wrote", out)
