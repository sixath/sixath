import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")

url = "http://10.137.211.84:29200/backend-union-access-*,backend-union_resource-*,backend-vm_manager-*,backend-cgsession-*,backend-sched-hub-*,backend-sched-planner-*/_search"
qid = "4103_yi4yu6ciljit"
body = {
    "size": 40,
    "sort": [{"@timestamp": "asc"}],
    "query": {
        "bool": {
            "must": [
                {
                    "query_string": {
                        "query": f'"{qid}"',
                        "default_field": "M",
                    }
                }
            ],
            "should": [
                {"term": {"L": "ERROR"}},
                {"term": {"L": "WARN"}},
                {
                    "query_string": {
                        "query": "fail OR error OR timeout OR refuse OR reject OR 失败",
                        "default_field": "M",
                    }
                },
            ],
            "minimum_should_match": 1,
        }
    },
}
req = urllib.request.Request(
    url,
    data=json.dumps(body).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(req, timeout=30) as r:
        data = json.loads(r.read().decode())
except Exception as e:
    print("ES FAIL", type(e).__name__, e)
    raise SystemExit(0)

hits = (data.get("hits") or {}).get("hits") or []
total = (data.get("hits") or {}).get("total")
print("errorish total", total, "returned", len(hits), "took", data.get("took"))
for h in hits:
    src = h.get("_source") or {}
    m = src.get("M") or src.get("message") or ""
    print(
        "---",
        h.get("_index"),
        src.get("T") or src.get("@timestamp"),
        src.get("LAPP"),
        src.get("L"),
    )
    print(m[:450])

# also fetch first+last of all hits
body2 = {
    "size": 5,
    "sort": [{"@timestamp": "asc"}],
    "query": {
        "query_string": {"query": f'"{qid}"', "default_field": "M"}
    },
}
req2 = urllib.request.Request(
    url,
    data=json.dumps(body2).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req2, timeout=30) as r:
    d2 = json.loads(r.read().decode())
hits2 = (d2.get("hits") or {}).get("hits") or []
print("\nFIRST hits total", (d2.get("hits") or {}).get("total"))
for h in hits2:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    print("F", src.get("T"), src.get("LAPP"), src.get("L"), m[:220])

body3 = dict(body2)
body3["sort"] = [{"@timestamp": "desc"}]
req3 = urllib.request.Request(
    url,
    data=json.dumps(body3).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req3, timeout=30) as r:
    d3 = json.loads(r.read().decode())
print("\nLAST hits")
for h in (d3.get("hits") or {}).get("hits") or []:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    print("L", src.get("T"), src.get("LAPP"), src.get("L"), m[:220])
