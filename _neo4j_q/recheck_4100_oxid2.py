# -*- coding: utf-8 -*-
import json
import sys
import urllib.request
import re

sys.stdout.reconfigure(encoding="utf-8")
ES = "http://10.137.211.84:29200"
FLOW = "4100_oxid8yvhdz6k"


def search(index, q, size=100):
    body = {
        "size": size,
        "sort": [{"@timestamp": "asc"}],
        "query": {"query_string": {"query": q, "default_field": "M", "analyze_wildcard": True}},
    }
    req = urllib.request.Request(
        f"{ES}/{index}/_search",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read().decode())


# Print full union_resource messages for this flow
d = search("backend-union_resource-*", f'"{FLOW}"', size=50)
hits = (d.get("hits") or {}).get("hits") or []
print("union_resource hits", len(hits))
for i, h in enumerate(hits):
    src = h.get("_source") or {}
    m = src.get("M") or ""
    print(f"\n==== UR#{i} {src.get('T')} len={len(m)} ====")
    # print whole if short, else find keys
    for pat in [
        r"image_info[:\s]*\[.*?\]",
        r"image_list[:\s]*\[.*?\]",
        r"resource_types[:\s]*\[.*?\]",
        r"other_resource_types[:\s]*\[.*?\]",
        r"gid[:\s]*\d+.*?image_info.*?\]",
        r"ComputeResourceTypes.*",
        r"GetImagesByGame.*",
        r"NewSelectResourceAndArea.*",
        r"fullflag.*",
        r"SelectArea.*",
    ]:
        for mobj in re.finditer(pat, m, flags=re.I):
            print(" REG", pat[:40], "=>", mobj.group(0)[:400])
    # also dump first 800 chars
    print(" HEAD:", m[:900].replace("\n", " "))

# Search image_info with gid nearby time
print("\n\n===== search image_info gid 2643 around flow =====")
d2 = search(
    "backend-union_resource-*",
    'gid:2643 AND image_info AND (10182 OR 10172)',
    size=30,
)
hits2 = (d2.get("hits") or {}).get("hits") or []
print("hits", (d2.get("hits") or {}).get("total"), "returned", len(hits2))
for h in hits2[:15]:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    ts = src.get("T")
    # only near 16:41
    if ts and "2026-09-21T16:4" not in str(ts):
        continue
    print("\n--", ts)
    for kw in ("image_info", "resource_types", "flow_id", "4100_oxid"):
        if kw in m:
            i = m.find(kw)
            print(m[max(0, i - 40) : i + 300])

# Also search flow + image_info without AND field assumptions
print("\n\n===== flow AND image_info =====")
d3 = search("backend-union_resource-*", f'"{FLOW}" AND image_info', size=20)
print("total", (d3.get("hits") or {}).get("total"))
for h in (d3.get("hits") or {}).get("hits") or []:
    m = (h.get("_source") or {}).get("M") or ""
    i = m.find("image_info")
    print(m[max(0, i - 80) : i + 400] if i >= 0 else m[:400])

# search access logs
print("\n\n===== union-access =====")
d4 = search("backend-union-access-*", f'"{FLOW}"', size=30)
print("total", (d4.get("hits") or {}).get("total"))
for h in (d4.get("hits") or {}).get("hits") or []:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    print("--", src.get("T"), m[:500])
