# -*- coding: utf-8 -*-
import json
import sys
import urllib.request
import re

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


# 1) Expand access ApplyToken urparam fully for this flow
print("===== access ApplyToken / other_resource_types =====")
d = search("backend-union-access-*", '"4100_oxid8yvhdz6k" AND (urparam OR other_resource OR urcode OR ApplyToken)', size=20)
for h in (d.get("hits") or {}).get("hits") or []:
    m = (h.get("_source") or {}).get("M") or ""
    ts = (h.get("_source") or {}).get("T")
    if "urparam" in m or "other_resource" in m or "urcode" in m.lower():
        print("\n--", ts)
        # extract urparam JSON-ish
        for pat in [r"urcode[^,}\"]*", r"other_resource_types[^]\]]*\]", r"image[^,}\"]*", r"resource_types[^]\]]*\]"]:
            for mo in re.finditer(pat, m):
                print(" ", mo.group(0)[:300])
        if "requestApplyToken" in m or "zoneAccessFacade" in m:
            i = m.find("urparam")
            print(m[max(0, i - 100) : i + 500])

# 2) Search image_info without flow, around 16:41 on that day for gid 2643 / biz 31205
print("\n\n===== image_info around time =====")
for q in [
    '"image_info" AND 2643 AND 10182',
    '"image_list" AND 10182 AND 10172',
    '"resource_types:" AND 3238264833 AND 3238133761',
    "GetImagesByGame AND 2643",
    "ComputeResourceTypes AND 2643",
    '"archive_mode" AND 10182',
]:
    d2 = search(
        "backend-union_resource-*",
        q,
        size=10,
        time_gte="2026-09-21T08:00:00.000Z",
        time_lte="2026-09-21T09:00:00.000Z",
    )
    total = (d2.get("hits") or {}).get("total")
    hits = (d2.get("hits") or {}).get("hits") or []
    print(f"\nQ={q} total={total} n={len(hits)}")
    for h in hits[:3]:
        src = h.get("_source") or {}
        m = src.get("M") or ""
        print(" ", src.get("T"))
        for kw in ("image_info", "image_list", "resource_types", "archive_mode", "flow_id", "4100_"):
            if kw in m:
                i = m.find(kw)
                print("   ", m[max(0, i - 30) : i + 280])
                break

# 3) Also try second flow
print("\n\n===== second flow 4100_1plixv4voe9h =====")
d3 = search("backend-union_resource-*", '"4100_1plixv4voe9h"', size=20)
print("total", (d3.get("hits") or {}).get("total"))
for h in (d3.get("hits") or {}).get("hits") or []:
    m = (h.get("_source") or {}).get("M") or ""
    if any(k in m for k in ("image_info", "image_version", "resource_type", "apply")):
        print((h.get("_source") or {}).get("T"), m[:400])

# 4) vm_manager getUrParam / resourceTypes line
print("\n\n===== vm getUrParam =====")
d4 = search("backend-vm_manager-*", '"4100_oxid8yvhdz6k" AND (getUrParam OR resourceTypes OR NewAllocVM)', size=20)
for h in (d4.get("hits") or {}).get("hits") or []:
    m = (h.get("_source") or {}).get("M") or ""
    print((h.get("_source") or {}).get("T"), m[:450])
