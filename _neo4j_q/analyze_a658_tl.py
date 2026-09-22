# -*- coding: utf-8 -*-
import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_a658.json", encoding="utf-8"))
items = d.get("items") or []

for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    tl = (m.get("metadata") or {}).get("timeline") or []
    print(f"\n######## ASSISTANT msg{i} timeline n={len(tl)} ########")
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            print(j, type(ev), str(ev)[:200])
            continue
        et = ev.get("type") or ev.get("kind") or ev.get("event")
        name = ev.get("name") or ev.get("tool") or ""
        # summarize
        keys = list(ev.keys())
        snippet = ""
        for k in ("input", "arguments", "args", "query", "content", "text", "result", "output", "summary"):
            if k in ev:
                snippet = json.dumps(ev[k], ensure_ascii=False)[:400]
                break
        if not snippet:
            snippet = json.dumps({k: ev[k] for k in keys if k not in ("id",)}, ensure_ascii=False)[:400]
        print(f"  [{j}] type={et} name={name} keys={keys[:12]}")
        print(f"       {snippet[:350]}")
