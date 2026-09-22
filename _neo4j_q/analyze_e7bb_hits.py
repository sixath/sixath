# -*- coding: utf-8 -*-
import json
import sys
import re

sys.stdout.reconfigure(encoding="utf-8")
d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e7bb.json", encoding="utf-8"))
tl = (d["items"][1].get("metadata") or {}).get("timeline") or []
ev = [e for e in tl if e.get("toolName") == "es_log_query" and (e.get("arguments") or {}).get("index") == "backend-union_resource-*"][0]
raw = ev.get("result")
data = json.loads(raw) if isinstance(raw, str) else raw
hits = data.get("hits") or []
print("meta count", data.get("count"), "has_more", data.get("has_more"), "continue_from", data.get("continue_from"), "hits_in_payload", len(hits), "result_str_len", len(raw) if isinstance(raw, str) else "n/a")

# list all M that contain key terms
for i, h in enumerate(hits):
    m = h.get("M") or ""
    if any(k in m for k in ("fullflag", "images:", "resource_types:", "SelectArea", "Output1", "Finish", "ugid and device")):
        print(f"\n--- hit[{i}] T={h.get('T')} ---")
        print(m[:500])

# Also check: are images: lines missing entirely from first 100 due to sort?
print("\n\nAny images: in payload?", any("images:" in (h.get("M") or "") for h in hits))
print("Any fullflag(2)?", any("fullflag(2)" in (h.get("M") or "") for h in hits))
print("Any fullflag(0)?", any("fullflag(0)" in (h.get("M") or "") for h in hits))
print("Any resource_types:[?", any("resource_types:[" in (h.get("M") or "") for h in hits))
