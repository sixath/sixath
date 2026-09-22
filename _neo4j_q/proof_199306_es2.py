# -*- coding: utf-8 -*-
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
ES = "http://10.137.211.84:29200"


def search(index, query, label, size=5):
    body = {
        "size": size,
        "sort": [{"@timestamp": "desc"}],
        "query": query,
        "track_total_hits": True,
    }
    url = f"{ES}/{index}/_search"
    req = urllib.request.Request(
        url, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"}, method="POST"
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        data = json.loads(resp.read().decode())
    hits = data.get("hits") or {}
    total = hits.get("total")
    arr = hits.get("hits") or []
    print(f"\n=== {label} ===")
    print("total", total, "returned", len(arr), "took", data.get("took"))
    for h in arr:
        src = h.get("_source") or {}
        msg = src.get("M") or ""
        print("-", h.get("_index"), src.get("T") or src.get("@timestamp"), src.get("LAPP"), src.get("L"))
        print(" ", str(msg)[:420].replace("\n", " "))


# mapping: does vmid exist as field?
url = f"{ES}/backend-sched-hub-2026.09.18/_mapping/field/vmid,M,vm_id,VmId"
req = urllib.request.Request(url)
with urllib.request.urlopen(req, timeout=30) as resp:
    mapping = json.loads(resp.read().decode())
print("=== mapping snippet ===")
print(json.dumps(mapping, ensure_ascii=False)[:1500])

qs = lambda q: {"query_string": {"query": q, "default_field": "M"}}

search("backend-sched-hub-*", qs("199306 AND UPSchedule"), "hub 199306 AND UPSchedule")
search("backend-sched-hub-*", qs("199306 AND OperationType"), "hub 199306 AND OperationType")
search("backend-sched-hub-*", qs("199306 AND (NoEmpty OR no empty OR not empty)"), "hub 199306 AND empty")
search("backend-cginstance-*", qs("199306 AND (NoEmpty OR UPSchedule OR 上线 OR 操作不合法)"), "cgi 199306 AND 上线/不合法")
search("backend-cginstance-*", qs("199306 AND VmOperateErr"), "cgi 199306 VmOperateErr")
search("backend-cginstance-*", qs("199306 AND onlineBatch"), "cgi 199306 onlineBatch")
search("backend-cginstance-*", qs('"需重启实例后才允许上线" AND 199306'), "cgi Chinese+vmid")
search("backend-*", qs('"需重启实例后才允许上线"'), "backend-* Chinese error only")
search("backend-sched-hub-*", qs("199306 AND (ClearOperationType OR operation_type)"), "hub 199306 clear/op_type")
search(
    "backend-sched-hub-2026.09.18",
    {"query_string": {"query": "vmid:199306 OR vm_id:199306 OR VmId:199306", "default_field": "M"}},
    "hub today field-or-M lucene vmid:199306",
)
