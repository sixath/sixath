# -*- coding: utf-8 -*-
import json
import sys
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")
d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_a658.json", encoding="utf-8"))
items = d.get("items") or []

tool_names = Counter()
for i, m in enumerate(items):
    meta = m.get("metadata") or {}
    role = m.get("role")
    print(f"=== msg{i} role={role} ===")
    print("meta keys", list(meta.keys())[:40])
    for k, v in meta.items():
        s = json.dumps(v, ensure_ascii=False)
        if k in ("toolCalls", "tools", "steps", "tool_calls", "events", "usage", "model", "skills", "skill", "toolResults"):
            print(f" meta.{k}:", s[:1500])
        elif "tool" in k.lower() or "step" in k.lower() or "skill" in k.lower():
            print(f" meta.{k}:", s[:800])
    c = m.get("content") or ""
    print("content_len", len(c) if isinstance(c, str) else type(c))
    if role == "user":
        print("USER:", c[:500])
    if role == "assistant" and isinstance(c, str):
        # look for root cause claims
        for marker in ["根因", "结论", "原因", "image_info", "rtSchedule", "10182", "10172", "GetImagesByGame", "URCode", "other_resource"]:
            if marker in c:
                idx = c.find(marker)
                print(f"  HIT {marker} @ {idx}: ...{c[max(0,idx-80):idx+200].replace(chr(10),' ')}...")
        print("TAIL:", c[-1500:].replace("\r", ""))
    print()

# Also dump any nested steps from all metas into a tools timeline
print("\n==== TOOL TIMELINE ====")
for i, m in enumerate(items):
    meta = m.get("metadata") or {}
    steps = meta.get("steps") or meta.get("toolCalls") or meta.get("events") or []
    if isinstance(steps, list):
        for s in steps:
            if isinstance(s, dict):
                name = s.get("name") or s.get("tool") or s.get("type") or s.get("kind")
                if name:
                    tool_names[str(name)] += 1
                    args = s.get("arguments") or s.get("input") or s.get("args") or {}
                    print(f"msg{i}", name, json.dumps(args, ensure_ascii=False)[:300])

print("tool_names", tool_names)
