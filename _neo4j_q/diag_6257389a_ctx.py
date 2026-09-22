# -*- coding: utf-8 -*-
import json

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_6257389a.json"
items = json.load(open(path, encoding="utf-8"))["items"]
focus = [1, 3, 5, 11, 13, 15, 17, 19, 21, 23]
for i in focus:
    m = items[i]
    tl = (m.get("metadata") or {}).get("timeline") or []
    content = m.get("content") or ""
    tool_chars = 0
    conf = []
    last_mc = None
    last_model = None
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        if ev.get("kind") == "tool":
            r = ev.get("result") or ""
            if not isinstance(r, str):
                r = json.dumps(r, ensure_ascii=False)
            tool_chars += len(r)
            if "confluence" in (ev.get("toolName") or ""):
                st = "ERR" if ("MCP error" in r or "Required at contentId" in r) else "OK"
                args = ev.get("arguments") or {}
                keys = list(args.keys()) if isinstance(args, dict) else [type(args).__name__]
                conf.append((len(r), st, keys))
        if ev.get("kind") == "model":
            last_mc = ev.get("messageCount")
            last_model = ev.get("model")
    print(
        f"msg[{i}] content={len(content)} events={len(tl)} "
        f"tool_result_chars={tool_chars} messageCount={last_mc} model={last_model}"
    )
    for n, st, keys in conf:
        print(f"  conf result_len={n} {st} arg_keys={keys}")
