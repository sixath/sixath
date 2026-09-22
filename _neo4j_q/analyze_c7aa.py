import json
from collections import Counter

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_c7aa.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
print("messages:", len(items))
for i, m in enumerate(items):
    role = m.get("role")
    content = (m.get("content") or "").replace("\n", " ")
    print(f"\n=== [{i}] {role} {m.get('createdAt')} ===")
    print("content:", content[:500])
    tl = (m.get("metadata") or {}).get("timeline") or []
    if not tl:
        continue
    tools = Counter()
    errs = []
    steps = []
    for ev in tl:
        if ev.get("kind") == "tool":
            name = ev.get("toolName") or "?"
            tools[name] += 1
            if ev.get("error"):
                errs.append((name, str(ev.get("error"))[:160], str(ev.get("arguments"))[:160]))
        if ev.get("kind") == "model" and isinstance(ev.get("step"), int):
            steps.append(ev.get("step"))
    print("tools:", dict(tools))
    print("rca_* count:", sum(v for k, v in tools.items() if str(k).startswith("rca_")))
    print("model steps:", (min(steps) if steps else None), "..", (max(steps) if steps else None), "n=", len(steps))
    for ev in tl:
        if ev.get("kind") == "model" and ev.get("step") == -1:
            print("end marker:", ev)
    if errs:
        print("errors:")
        for e in errs[:12]:
            print(" ", e)
    # sample rca args
    for ev in tl:
        if ev.get("toolName") in ("rca_grep", "rca_glob", "rca_read") and ev.get("phase") in ("completed", "started", None):
            print(" sample", ev.get("toolName"), "args", ev.get("arguments"), "err", bool(ev.get("error")))
