# -*- coding: utf-8 -*-
import json
import sys
import re

sys.stdout.reconfigure(encoding="utf-8")
d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e7bb.json", encoding="utf-8"))
tl = (d["items"][1].get("metadata") or {}).get("timeline") or []
ev = [
    e
    for e in tl
    if e.get("toolName") == "es_log_query"
    and (e.get("arguments") or {}).get("index") == "backend-union_resource-*"
][0]
raw = ev.get("result")
print("len", len(raw), "truncated_flag", ev.get("truncated"))
print("endswith", repr(raw[-120:]))
print("HEAD", raw[:600])
for kw in [
    "fullflag",
    "images:",
    "resource_types:",
    "SelectArea",
    "Output1",
    "ugid and device",
    "has_more",
    "continue_from",
    "count",
]:
    print(kw, "pos", raw.find(kw))

print("\n=== ApplyArea M snippets in truncated payload ===")
for m in re.finditer(r'"M":"(\[ApplyArea\][^"]{0,500})', raw):
    s = m.group(1)
    # unescape common sequences lightly
    s = s.replace("\\u003c", "<").replace("\\u003e", ">")
    if any(k in s for k in ("fullflag", "SelectArea", "images", "resource_types", "Output", "Finish", "ugid")):
        print(s[:400])
        print("---")
