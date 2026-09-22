import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8d647233.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    print("=" * 80)
    print("MSG", i, "id=", m.get("id"), "created=", m.get("createdAt"))
    print("content_head:", (m.get("content") or "")[:800])
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") or []
    print("timeline events", len(tl), "meta_keys", list(meta.keys())[:30])
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            continue
        k = ev.get("kind") or ev.get("type") or "?"
        name = ev.get("toolName") or ev.get("name") or ""
        status = ev.get("status") or ""
        err = str(ev.get("error") or "")[:200]
        args = ev.get("args") or ev.get("input") or ev.get("arguments") or ev.get("toolInput") or ev.get("params")
        result = ev.get("result") or ev.get("output") or ev.get("toolResult") or ev.get("content")
        if k in ("think", "model"):
            continue
        print(f"  [{j}] kind={k} name={name} status={status}")
        if err:
            print("     error", err)
        if args is not None:
            s = json.dumps(args, ensure_ascii=False)
            print("     args", s[:500])
        if result is not None:
            if isinstance(result, (dict, list)):
                s = json.dumps(result, ensure_ascii=False)
            else:
                s = str(result)
            print("     result", s[:400])
        if j == 0:
            print("     keys", list(ev.keys()))
