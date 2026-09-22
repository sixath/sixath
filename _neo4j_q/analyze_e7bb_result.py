# -*- coding: utf-8 -*-
import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e7bb.json", encoding="utf-8"))
items = d.get("items") or []

# dump union_resource 0_ query result thoroughly
m = items[1]
tl = (m.get("metadata") or {}).get("timeline") or []
for j, ev in enumerate(tl):
    if ev.get("kind") != "tool":
        continue
    args = ev.get("arguments") or {}
    name = ev.get("toolName")
    if name != "es_log_query":
        continue
    q = str(args.get("query") or "")
    idx = args.get("index")
    print(f"\n===== tool[{j}] index={idx} query={q} truncated={ev.get('truncated')} =====")
    result = ev.get("result")
    # result might be string path or dict
    print("result type", type(result).__name__)
    if isinstance(result, str):
        print("result str len", len(result))
        print(result[:2000])
        # search keywords
        for kw in ("fullflag", "images:", "resource_types:", "SelectArea", "Output1", "10182 10172", "3238264833"):
            print(f"  contains {kw}:", kw in result)
    elif isinstance(result, dict):
        s = json.dumps(result, ensure_ascii=False)
        print("result dict len", len(s), "keys", list(result.keys())[:20])
        for kw in ("fullflag", "images:", "resource_types:", "SelectArea", "Output1", "hit_status", "path", "truncated", "total"):
            if kw in s:
                print(f"  has {kw}")
        # if path to file
        path = result.get("path") or result.get("result_path") or result.get("file")
        print("path-ish", path)
        print(s[:2500])
