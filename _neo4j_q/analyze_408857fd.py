import json
import sys
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_408857fd.json"
with open(path, encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
print("top keys", list(data.keys()))
print("n items", len(items))

for i, m in enumerate(items):
    print("\n" + "=" * 80)
    print(f"MSG[{i}] role={m.get('role')} id={m.get('id')} created={m.get('createdAt')}")
    print("keys", list(m.keys()))
    content = m.get("content") or ""
    print("content:\n", content)
    meta = m.get("metadata") or {}
    if not isinstance(meta, dict):
        print("meta type", type(meta))
        continue
    print("meta keys", list(meta.keys()))
    for k, v in meta.items():
        if k == "timeline":
            continue
        print(f"  META {k}={json.dumps(v, ensure_ascii=False)[:500]}")
    tl = meta.get("timeline")
    if not isinstance(tl, list):
        print("no timeline")
        continue
    print("timeline events", len(tl))
    kinds = Counter()
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            print(f"  [{j}] non-dict {type(ev)}")
            continue
        kind = ev.get("kind") or ev.get("type") or ev.get("event") or "?"
        kinds[kind] += 1
        name = ev.get("toolName") or ev.get("name") or ""
        tool = ev.get("tool") if isinstance(ev.get("tool"), dict) else {}
        if tool:
            name = tool.get("name") or name
        status = ev.get("status") or tool.get("ok")
        err = ev.get("error") or tool.get("error") or ev.get("message")
        dur = ev.get("durationMs") or ev.get("duration_ms") or ev.get("duration")
        brief = {
            k: ev.get(k)
            for k in (
                "kind",
                "type",
                "event",
                "name",
                "toolName",
                "status",
                "phase",
                "step",
                "iteration",
            )
            if k in ev
        }
        extra = ""
        # include small text fields
        for k in ("content", "text", "delta", "thought", "reason"):
            if isinstance(ev.get(k), str) and ev[k].strip():
                extra += f" {k}={ev[k][:120]!r}"
        args = ev.get("args") or ev.get("input") or tool.get("args") or tool.get("input")
        result = ev.get("result") or ev.get("output") or tool.get("result") or tool.get("output")
        if args:
            extra += f" args={json.dumps(args, ensure_ascii=False)[:180]}"
        if result is not None:
            rs = result if isinstance(result, str) else json.dumps(result, ensure_ascii=False)
            extra += f" result={rs[:220]}"
        if err:
            extra += f" ERR={str(err)[:200]}"
        print(f"  [{j}] {kind} name={name} status={status} dur={dur} keys={list(ev.keys())[:12]}{extra}")
    print("kind counts", dict(kinds))
