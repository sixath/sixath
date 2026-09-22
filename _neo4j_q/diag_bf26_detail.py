# -*- coding: utf-8 -*-
import json
from collections import Counter

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_bf26.json"
with open(path, encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
print("messages:", len(items))
for i, m in enumerate(items):
    role = m.get("role")
    content = m.get("content") or ""
    print("\n" + "=" * 80)
    print(f"[{i}] role={role} created={m.get('createdAt')} content_len={len(content)}")
    print("--- CONTENT ---")
    print(content)
    md = m.get("metadata") or {}
    tl = md.get("timeline") or []
    if not isinstance(tl, list) or not tl:
        continue
    print("--- TIMELINE", len(tl), "events ---")
    tools = Counter()
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        kind = ev.get("kind")
        name = ev.get("toolName") or ""
        step = ev.get("step")
        phase = ev.get("phase")
        err = ev.get("error")
        if kind == "tool":
            tools[name] += 1
        brief = {
            "kind": kind,
            "tool": name,
            "step": step,
            "phase": phase,
        }
        if err:
            brief["error"] = str(err)[:200]
        args = ev.get("arguments")
        if isinstance(args, dict):
            # keep short
            brief["args"] = {k: (str(v)[:120] if not isinstance(v, (int, float, bool)) else v) for k, v in list(args.items())[:8]}
        elif args:
            brief["args"] = str(args)[:200]
        res = ev.get("result")
        if isinstance(res, str):
            brief["result"] = res[:400]
        elif isinstance(res, dict):
            brief["result_keys"] = list(res.keys())[:20]
            s = json.dumps(res, ensure_ascii=False)
            brief["result"] = s[:400]
        elif res is not None:
            brief["result"] = str(res)[:200]
        # model content
        if kind == "model":
            for k in ("content", "text", "delta", "finishReason", "finish_reason", "reason"):
                if ev.get(k):
                    brief[k] = str(ev.get(k))[:300]
        print(json.dumps(brief, ensure_ascii=False))
    print("tools:", dict(tools))

# write last assistant timeline compact
asst = items[-1]
out = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_bf26_last.json"
with open(out, "w", encoding="utf-8") as f:
    json.dump(asst, f, ensure_ascii=False, indent=2)
print("wrote", out)
