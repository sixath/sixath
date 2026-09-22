# -*- coding: utf-8 -*-
import json
import sys
import urllib.error
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
ES = "http://10.137.211.84:29200"


def req(method, path, body=None, timeout=40):
    url = ES + path
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            raw = resp.read().decode()
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        raw = e.read().decode(errors="replace")
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = {"raw": raw[:500]}
        return e.code, parsed
    except Exception as e:
        return 0, {"error": f"{type(e).__name__}: {e}"}


def cat(pattern):
    status, data = req("GET", f"/_cat/indices/{pattern}?format=json&h=index,docs.count,store.size")
    print(f"\n=== CAT {pattern} status={status} ===")
    if isinstance(data, list):
        print("n=", len(data))
        for row in data[:15]:
            print(row)
        if len(data) > 15:
            print("...", len(data) - 15, "more")
        total_docs = 0
        for row in data:
            try:
                total_docs += int(str(row.get("docs.count") or "0").replace(",", ""))
            except Exception:
                pass
        print("sum docs.count (first page rows)", total_docs, "rows", len(data))
    else:
        print(json.dumps(data, ensure_ascii=False)[:600])


def search(index, query, label, size=3):
    status, data = req("POST", f"/{index}/_search", {
        "size": size,
        "sort": [{"@timestamp": "desc"}],
        "query": query,
        "track_total_hits": True,
    })
    hits = (data.get("hits") or {})
    total = hits.get("total")
    arr = hits.get("hits") or []
    print(f"\n=== {label} ===")
    print("status", status, "total", total, "returned", len(arr), "took", data.get("took"))
    if data.get("error"):
        print("error", json.dumps(data.get("error"), ensure_ascii=False)[:400])
    idxs = {}
    for h in arr:
        idxs[h.get("_index")] = idxs.get(h.get("_index"), 0) + 1
        src = h.get("_source") or {}
        msg = src.get("M") or src.get("message") or src.get("msg") or ""
        print("-", h.get("_index"), src.get("T") or src.get("@timestamp"), src.get("LAPP"), src.get("L"))
        print(" ", str(msg)[:350].replace("\n", " "))
    if idxs:
        print("hit indices", idxs)


# 1) which indices exist
for p in [
    "cgschedule-*",
    "cginstance-*",
    "vm-manager-*",
    "app-*",
    "backend-sched-hub-*",
    "backend-cginstance-*",
    "backend-cgschedule*",
    "backend-sched*",
]:
    cat(p)

# 2) agent's actual queries vs correct ones
search("cgschedule-*", {"query_string": {"query": "vmid:199306"}}, "AGENT: cgschedule-* vmid:199306")
search("cginstance-*", {"query_string": {"query": "199306"}}, "AGENT: cginstance-* 199306")
search("backend-sched-hub-*", {"query_string": {"query": "vmid:199306"}}, "WRONG FIELD: backend-sched-hub-* vmid:199306")
search(
    "backend-sched-hub-*",
    {"query_string": {"query": "199306", "default_field": "M"}},
    "CORRECT: backend-sched-hub-* M:199306",
)
search(
    "backend-sched-hub-*",
    {"query_string": {"query": '"vm operation type not empty"', "default_field": "M"}},
    "CORRECT: backend-sched-hub-* M:vm operation type not empty",
)
search(
    "backend-sched-hub-*",
    {
        "bool": {
            "must": [
                {"query_string": {"query": "199306", "default_field": "M"}},
                {"query_string": {"query": '"vm operation type not empty" OR UPSchedule OR OperationType', "default_field": "M"}},
            ]
        }
    },
    "CORRECT: backend-sched-hub-* 199306 AND (operation type|UPSchedule)",
)
search(
    "backend-cginstance-*",
    {"query_string": {"query": '"操作不合法，需重启实例后才允许上线"', "default_field": "M"}},
    "CORRECT: backend-cginstance-* Chinese error",
)
search(
    "backend-cginstance-*",
    {"query_string": {"query": "199306", "default_field": "M"}},
    "CORRECT: backend-cginstance-* M:199306",
)
search(
    "backend-*",
    {"query_string": {"query": "199306 AND (\"vm operation type not empty\" OR UpScheduleResultOperationTypeNoEmpty)", "default_field": "M"}},
    "CORRECT: backend-* 199306 AND english RPC",
)
