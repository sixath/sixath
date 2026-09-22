import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_459d3153.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    print("=" * 80)
    print("MSG", i, m.get("createdAt"))
    tl = (m.get("metadata") or {}).get("timeline") or []
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            continue
        k = ev.get("kind") or "?"
        name = ev.get("toolName") or ev.get("name") or ""
        if k in ("think", "model") and not name:
            continue
        args = ev.get("args") or ev.get("input") or ev.get("arguments") or {}
        result = ev.get("result") or ev.get("output")
        err = ev.get("error")
        print(f"  [{j}] {k} {name}")
        if args:
            print("     args", json.dumps(args, ensure_ascii=False)[:600])
        if err:
            print("     error", str(err)[:300])
        if result is not None:
            s = json.dumps(result, ensure_ascii=False) if isinstance(result, (dict, list)) else str(result)
            print("     result", s[:350])
