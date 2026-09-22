import json
import urllib.request
from collections import Counter

sid = "75f7b4da-a700-4b27-8be9-787aa1c895d5"
url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
try:
    with urllib.request.urlopen(req, timeout=60) as resp:
        data = json.load(resp)
except Exception as e:
    print("ERR", type(e).__name__, e)
    raise

items = data.get("items") or []
print("messages:", len(items))
print("keys:", list(data.keys())[:20])
out = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7.json"
with open(out, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)
print("wrote", out)

for i, m in enumerate(items):
    role = m.get("role")
    content = (m.get("content") or "").replace("\n", " ")
    print(f"\n=== [{i}] {role} {m.get('createdAt')} ===")
    print("content_len", len(m.get("content") or ""))
    print("content:", content[:800])
    md = m.get("metadata") or {}
    tl = md.get("timeline") or []
    print("timeline_events", len(tl) if isinstance(tl, list) else type(tl))
    print("metadata_keys", list(md.keys())[:30])
    if not isinstance(tl, list) or not tl:
        continue
    tools = Counter()
    errs = []
    steps = []
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        if ev.get("kind") == "tool":
            name = ev.get("toolName") or "?"
            tools[name] += 1
            if ev.get("error"):
                errs.append((name, str(ev.get("error"))[:200], str(ev.get("phase"))))
        if ev.get("kind") == "model" and isinstance(ev.get("step"), int):
            steps.append(ev.get("step"))
    print("tools:", dict(tools))
    print("model steps:", (min(steps) if steps else None), "..", (max(steps) if steps else None), "n=", len(steps))
    for ev in tl:
        if isinstance(ev, dict) and ev.get("kind") == "model" and ev.get("step") == -1:
            print("end marker:", {k: ev.get(k) for k in list(ev)[:20]})
    if errs:
        print("errors:")
        for e in errs[:20]:
            print(" ", e)
    print("last 8 events:")
    for ev in tl[-8:]:
        if not isinstance(ev, dict):
            print(" ", ev)
            continue
        err = str(ev.get("error"))[:80] if ev.get("error") else ""
        print(" ", ev.get("kind"), ev.get("toolName"), ev.get("step"), ev.get("phase"), err, list(ev.keys())[:10])
