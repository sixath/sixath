# -*- coding: utf-8 -*-
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
ES = "http://10.137.211.84:29200"


def search(index, query, label, size=4):
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
    with urllib.request.urlopen(req, timeout=90) as resp:
        data = json.loads(resp.read().decode())
    hits = data.get("hits") or {}
    print(f"\n=== {label} ===")
    print("total", hits.get("total"), "took", data.get("took"))
    if data.get("error"):
        print("ERR", json.dumps(data["error"], ensure_ascii=False)[:300])
    for h in (hits.get("hits") or []):
        src = h.get("_source") or {}
        msg = src.get("M") or ""
        print("-", h.get("_index"), src.get("T") or src.get("@timestamp"), src.get("LAPP"), src.get("L"))
        print(" ", str(msg)[:450].replace("\n", " "))


def qsM(q):
    return {"query_string": {"query": q, "default_field": "M"}}


# vm_id field exists
search("backend-sched-hub-*", {"term": {"vm_id": "199306"}}, "term vm_id=199306")
search("backend-sched-hub-*", {"term": {"vm_id.keyword": "199306"}}, "term vm_id.keyword=199306")
search("backend-sched-hub-*", qsM("199306 AND (\"UPSchedule\" OR UpSchedule OR up_schedule)"), "hub UPSchedule variants")
search("backend-cginstance-*", qsM("199306 AND (\"UPSchedule\" OR UpSchedule OR NoEmptyVmids OR onlineBatch)"), "cgi UPSchedule variants")
search("backend-cginstance-*", qsM("199306 AND (\"操作不合法\" OR 上线失败 OR OnlineVm OR online vm)"), "cgi 操作不合法/上线")
search("backend-*", qsM("\"vm operation type not empty\""), "any backend exact english msg")
search("backend-*", qsM("UpScheduleResultOperationTypeNoEmpty"), "any backend enum name")
search("backend-sched-hub-*", qsM("199306 AND (offline OR OfflineVMs OR DynamicData)"), "hub 199306 offline/dynamic")
search(
    "backend-cginstance-*",
    {
        "bool": {
            "must": [
                {"query_string": {"query": "199306", "default_field": "M"}},
                {"term": {"L": "error"}},
            ]
        }
    },
    "cgi 199306 level=error",
)
