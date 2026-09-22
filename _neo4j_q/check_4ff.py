import json
import urllib.request
from collections import Counter

h = {"Authorization": "Bearer dev-bootstrap-token"}
sid = "4ff845fc-c418-4c7e-b299-fac1e6319ef1"
url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages"
data = json.load(urllib.request.urlopen(urllib.request.Request(url, headers=h), timeout=60))
items = data.get("items") or []
print("n", len(items))
path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_4ff.json"
with open(path, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)

for i, m in enumerate(items):
    role = m.get("role")
    c = (m.get("content") or "").replace("\n", " ")
    tl = (m.get("metadata") or {}).get("timeline") or []
    tools = Counter(ev.get("toolName") for ev in tl if ev.get("kind") == "tool")
    steps = [ev.get("step") for ev in tl if ev.get("kind") == "model"]
    print(
        f"[{i}] {role} {m.get('createdAt')} content_len={len(m.get('content') or '')} "
        f"timeline={len(tl)} tools={dict(tools)} steps={steps[:3]}..{steps[-3:] if steps else []}"
    )
    print("  head:", c[:200])
    for ev in tl:
        if ev.get("kind") == "model" and ev.get("step") == -1:
            print("  end:", ev)
    errs = [
        (ev.get("toolName"), str(ev.get("error"))[:120])
        for ev in tl
        if ev.get("kind") == "tool" and ev.get("error")
    ]
    if errs:
        print("  errs:", errs[:8])
