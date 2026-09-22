# -*- coding: utf-8 -*-
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
ES = "http://10.137.211.84:29200"


def search(index, q, size=50):
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


q = '"0_oxid8yvhdz6k" AND (SelectArea OR resource_types OR images OR NewSelect OR ApplyArea)'
d = search("backend-union_resource-*", q, size=50)
print("total", (d.get("hits") or {}).get("total"))
for h in (d.get("hits") or {}).get("hits") or []:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    print("\n====", src.get("T"), "====")
    print(m[:1200])
