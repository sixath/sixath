# -*- coding: utf-8 -*-
import json
import sys
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")
d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e7bb.json", encoding="utf-8"))
items = d.get("items") or []

for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    tl = (m.get("metadata") or {}).get("timeline") or []
    print(f"\n######## msg{i} timeline n={len(tl)} ########")
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict) or ev.get("kind") != "tool":
            continue
        name = ev.get("toolName") or ""
        args = ev.get("arguments") or {}
        truncated = ev.get("truncated")
        phase = ev.get("phase")
        result = ev.get("result")
        rsum = ""
        if isinstance(result, dict):
            rsum = json.dumps(result, ensure_ascii=False)[:400]
        elif isinstance(result, str):
            rsum = result[:400]
        print(f"  [{j}] {name} phase={phase} trunc={truncated}")
        print(f"       args={json.dumps(args, ensure_ascii=False)[:350]}")
        # check if 0_ or fullflag or images in result
        rs = rsum or ""
        marks = []
        for k in ("0_oxid", "4100_oxid", "fullflag", "images:", "resource_types:", "image_info", "SelectArea", "10182", "10172"):
            if k in rs or k in json.dumps(args, ensure_ascii=False):
                marks.append(k)
        if marks:
            print(f"       marks={marks}")
        if "0_oxid" in rs or "fullflag" in rs or "images:" in rs:
            print(f"       RESULT_SNIP={rs[:500]}")

# Also check if assistant content mentions fullflag / images order
c = items[1].get("content") or ""
for kw in ("0_oxid", "fullflag", "images:", "10182 10172 10202", "3238264833 3238133761", "判满", "FULL", "预启动"):
    print(f"msg1 has [{kw}]:", kw in c)
c3 = items[3].get("content") or ""
for kw in ("0_oxid", "fullflag", "images:", "判满", "FULL", "预启动", "GetNormalVMNode"):
    print(f"msg3 has [{kw}]:", kw in c3)
