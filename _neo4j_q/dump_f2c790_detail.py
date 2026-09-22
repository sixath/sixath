import json
import urllib.request

sid = "f2c790d6-671d-499f-8b4e-0c53f86e4df4"
url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages?limit=100"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
with urllib.request.urlopen(req, timeout=30) as r:
    data = json.loads(r.read().decode())

asst = data["items"][-1]
tl = (asst.get("metadata") or {}).get("timeline") or []
print("created", asst.get("createdAt"), "clen", len(asst.get("content") or ""))
print("content:\n", asst.get("content"))
print("\n=== full timeline ===")
for ev in tl:
    kind = ev.get("kind")
    name = ev.get("toolName") or ev.get("model") or ""
    phase = ev.get("phase")
    step = ev.get("step")
    dur = ev.get("durationMs")
    err = ev.get("error") or ""
    print(f"step={step} {kind} {phase} {name} dur={dur}")
    if err:
        print("  ERR", err[:400])
    if name in ("es_log_query", "http_request") and phase == "completed":
        res = ev.get("result")
        rs = res if isinstance(res, str) else json.dumps(res, ensure_ascii=False) if res is not None else ""
        print("  result", rs[:400].replace("\n", " "))
        args = ev.get("arguments")
        print("  args", json.dumps(args, ensure_ascii=False)[:300] if args else None)
