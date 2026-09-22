import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")

url = "http://10.137.211.84:29200/backend-union-access-*,backend-union_resource-*,backend-vm_manager-*,backend-cgsession-*,backend-sched-hub-*,backend-sched-planner-*/_search"
qid = "4103_yi4yu6ciljit"
body = {
    "size": 200,
    "sort": [{"@timestamp": "asc"}],
    "_source": ["T", "LAPP", "L", "M", "LFILE"],
    "query": {"query_string": {"query": f'"{qid}"', "default_field": "M"}},
}
req = urllib.request.Request(
    url,
    data=json.dumps(body).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req, timeout=60) as r:
    data = json.loads(r.read().decode())
hits = (data.get("hits") or {}).get("hits") or []
print("total", (data.get("hits") or {}).get("total"), "returned", len(hits))
keys = (
    "fail",
    "error",
    "timeout",
    "refuse",
    "reject",
    "失败",
    "release",
    "recycle",
    "assign",
    "vmid",
    "Lock",
    "Unlock",
    "startGame",
    "stop",
    "queue",
    "not user current",
    "success",
    "code",
)
for h in hits:
    src = h.get("_source") or {}
    m = src.get("M") or ""
    flag = ""
    low = m.lower()
    for k in keys:
        if k.lower() in low or k in m:
            flag = k
            break
    if flag or src.get("L") in ("ERROR", "WARN"):
        print(
            f"{src.get('T')} {src.get('LAPP')} {src.get('L')} [{flag}] {m[:280]}"
        )

print("\n==== ALL condensed ====")
for h in hits:
    src = h.get("_source") or {}
    m = (src.get("M") or "").replace("\n", " ")
    print(f"{src.get('T')[-12:]} {src.get('LAPP'):16} {src.get('L'):5} {m[:160]}")
