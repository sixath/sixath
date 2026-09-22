# -*- coding: utf-8 -*-
"""Use sixath zj-elk ES endpoint to re-investigate 4100_oxid8yvhdz6k."""
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")

ES = "http://10.137.211.84:29200"  # zj-elk tool endpoint from sixath agent e810
FLOW = "4100_oxid8yvhdz6k"
OUT = r"E:\workspace\github\sixath\sixath\_neo4j_q\recheck_4100_oxid.jsonl"


def es_search(index: str, query: str, size: int = 80):
    url = f"{ES}/{index}/_search"
    body = {
        "size": size,
        "sort": [{"@timestamp": "asc"}],
        "query": {
            "query_string": {"query": query, "default_field": "M", "analyze_wildcard": True}
        },
        "_source": ["@timestamp", "T", "LAPP", "L", "M", "flow_id", "trace_id", "S"],
    }
    req = urllib.request.Request(
        url,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read().decode())


def dump_hits(label, data, keywords=None):
    hits = (data.get("hits") or {}).get("hits") or []
    total = (data.get("hits") or {}).get("total")
    print(f"\n===== {label} total={total} returned={len(hits)} =====")
    with open(OUT, "a", encoding="utf-8") as f:
        for h in hits:
            src = h.get("_source") or {}
            m = src.get("M") or ""
            row = {
                "label": label,
                "index": h.get("_index"),
                "ts": src.get("T") or src.get("@timestamp"),
                "app": src.get("LAPP") or src.get("S"),
                "level": src.get("L"),
                "M": m,
            }
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
            if keywords:
                if not any(k.lower() in m.lower() for k in keywords):
                    continue
            # print compact
            print(f"-- {row['ts']} {row['app']}")
            # highlight interesting snippets
            for kw in (
                "image_info",
                "image_list",
                "resource_types",
                "rtScheduleInfos",
                "NewAllocVM",
                "GetImagesByGame",
                "NewSelectResourceAndArea",
                "other_resource",
                "URCode",
                "10182",
                "10172",
                "3238264833",
                "3238133761",
                "fullflag",
                "SelectArea",
                "assign succ",
                "ImageVersion",
            ):
                if kw in m:
                    i = m.find(kw)
                    print(f"  HIT[{kw}]: ...{m[max(0,i-60):i+220]}...")
                    break
            else:
                if keywords:
                    print(f"  {m[:350]}")


open(OUT, "w", encoding="utf-8").write("")

# 1) union_resource apply/select evidence
d1 = es_search(
    "backend-union_resource-*",
    f'"{FLOW}" AND (image_info OR image_list OR resource_types OR NewSelectResourceAndArea OR SelectArea OR ApplyArea OR ComputeResourceTypes OR GetImagesByGame OR other_resource)',
    size=100,
)
dump_hits("union_resource focused", d1)

# 2) broader union_resource
d2 = es_search("backend-union_resource-*", f'"{FLOW}"', size=100)
dump_hits(
    "union_resource all",
    d2,
    keywords=["image_info", "resource_type", "10182", "10172", "Apply", "Select", "full"],
)

# 3) vm_manager assign order
d3 = es_search(
    "backend-vm_manager-*",
    f'"{FLOW}" AND (rtScheduleInfos OR NewAllocVM OR GetPreLaunchedOne OR GetOne OR ImageVersion OR assign)',
    size=80,
)
dump_hits("vm_manager focused", d3)

print("\nWrote", OUT)
