import json
import urllib.request
from collections import Counter

sid = "3b15fb7b-c980-4356-be1b-43eb5078abfb"
url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
with urllib.request.urlopen(req, timeout=60) as resp:
    data = json.load(resp)

out = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_3b15.json"
with open(out, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)
print("wrote", out, "messages", len(data.get("items") or []))

for i, m in enumerate(data.get("items") or []):
    role = m.get("role")
    content = (m.get("content") or "").replace("\n", " ")
    print(f"\n=== [{i}] {role} {m.get('createdAt')} len={len(m.get('content') or '')} ===")
    print(content[:600])
    tl = (m.get("metadata") or {}).get("timeline") or []
    print("timeline", len(tl) if isinstance(tl, list) else type(tl))
    if not isinstance(tl, list):
        continue
    tools = Counter()
    for ev in tl:
        if ev.get("kind") == "tool":
            tools[ev.get("toolName") or "?"] += 1
    print("tools", dict(tools))
    for ev in tl:
        if ev.get("kind") != "tool":
            continue
        name = ev.get("toolName")
        if name in ("load_skill", "skill_view", "skills_list", "read_skill_file"):
            args = ev.get("arguments")
            res = ev.get("result")
            rs = res if isinstance(res, str) else json.dumps(res, ensure_ascii=False) if res is not None else ""
            print(f"  step={ev.get('step')} {name} phase={ev.get('phase')} args={args}")
            print(f"    result_len={len(rs)} head={rs[:180]!r}")
