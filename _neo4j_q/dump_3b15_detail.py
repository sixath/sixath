import json
import urllib.request

sid = "3b15fb7b-c980-4356-be1b-43eb5078abfb"
url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
with urllib.request.urlopen(req, timeout=60) as r:
    data = json.loads(r.read().decode())

items = data["items"]
print("n", len(items))
for i, m in enumerate(items):
    print(f"\n=== [{i}] {m.get('role')} {m.get('createdAt')} clen={len(m.get('content') or '')} ===")
    print((m.get("content") or "")[:800].replace("\n", " | "))

asst = items[1]
tl = (asst.get("metadata") or {}).get("timeline") or []
print("\n=== timeline order (tool only) ===")
for ev in tl:
    if ev.get("kind") != "tool":
        continue
    name = ev.get("toolName")
    args = ev.get("arguments")
    phase = ev.get("phase")
    err = ev.get("error") or ""
    res = ev.get("result")
    rs = res if isinstance(res, str) else (json.dumps(res, ensure_ascii=False) if res is not None else "")
    arg_s = json.dumps(args, ensure_ascii=False) if args is not None else ""
    print(f"step={ev.get('step')} {name} {phase} dur={ev.get('durationMs')} args={arg_s[:180]}")
    if name == "list_tools":
        for tname in ["jaeger_trace", "es_log_query", "rca_grep", "rca_glob", "rca_read", "http_request", "load_skill", "skill_view"]:
            print(f"    catalog {tname}: {rs.count(tname)}")
        print("    head", rs[:220].replace("\n", " "))
    if err:
        print("    ERR", err[:300])

print("\n=== model text snippets ===")
for ev in tl:
    if ev.get("kind") == "model" and ev.get("phase") in ("completed", "delta", None):
        pass
