# -*- coding: utf-8 -*-
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
ES = "http://10.137.211.84:29200"


def search(index, q, size=50, time_gte=None, time_lte=None):
    must = [{"query_string": {"query": q, "default_field": "M", "analyze_wildcard": True}}]
    if time_gte or time_lte:
        rng = {}
        if time_gte:
            rng["gte"] = time_gte
        if time_lte:
            rng["lte"] = time_lte
        must.append({"range": {"@timestamp": rng}})
    body = {
        "size": size,
        "sort": [{"@timestamp": "asc"}],
        "query": {"bool": {"must": must}},
    }
    req = urllib.request.Request(
        f"{ES}/{index}/_search",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read().decode())


# Around apply time 16:41:09 CST = 08:41:09 UTC
print("===== ugid 2643 resource_types near 16:41 =====")
d = search(
    "backend-union_resource-*",
    'ugid:2643 AND resource_types AND images',
    size=30,
    time_gte="2026-09-21T08:40:00.000Z",
    time_lte="2026-09-21T08:45:00.000Z",
)
print("total", (d.get("hits") or {}).get("total"))
for h in (d.get("hits") or {}).get("hits") or []:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    print("\n--", src.get("T"))
    print(m[:600])

print("\n\n===== oxid8yvhdz6k without area prefix =====")
for q in [
    "oxid8yvhdz6k AND (images OR resource_types OR image_info OR archive_mode)",
    "oxid8yvhdz6k",
    '"0_oxid8yvhdz6k"',
]:
    d2 = search("backend-union_resource-*", q, size=20)
    print(f"Q={q} total={(d2.get('hits') or {}).get('total')}")
    for h in (d2.get("hits") or {}).get("hits") or []:
        m = (h.get("_source") or {}).get("M") or ""
        if any(k in m for k in ("images:", "resource_types:", "image_info", "archive_mode", "fullflag", "SelectArea")):
            print((h.get("_source") or {}).get("T"), m[:500])

print("\n\n===== SelectArea / fullflag near apply for 3238264833 =====")
d3 = search(
    "backend-union_resource-*",
    "3238264833 AND (fullflag OR SelectArea OR FULLTYPE OR LimitNum OR available)",
    size=20,
    time_gte="2026-09-21T08:40:00.000Z",
    time_lte="2026-09-21T08:42:00.000Z",
)
print("total", (d3.get("hits") or {}).get("total"))
for h in (d3.get("hits") or {}).get("hits") or []:
    src = h.get("_source") or {}
    print(src.get("T"), (src.get("M") or "")[:450])

# second flow apply result
print("\n\n===== second flow apply image =====")
d4 = search("backend-union_resource-*", '"4100_1plixv4voe9h" AND apply', size=5)
for h in (d4.get("hits") or {}).get("hits") or []:
    print((h.get("_source") or {}).get("M") or "")[:700]
