import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")

sid = "459d3153-0dd1-4a05-aa57-492dd1ef0e8a"
url = f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages?limit=200"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
with urllib.request.urlopen(req, timeout=30) as r:
    data = json.loads(r.read().decode())

items = data.get("items") or []
print("n=", len(items))
# last 3 messages in detail
for i, m in enumerate(items[-4:]):
    idx = len(items) - 4 + i
    print("\n" + "=" * 80)
    print(f"[{idx}] {m.get('role')} {m.get('createdAt')} clen={len(m.get('content') or '')}")
    print("content:", (m.get("content") or "")[:500].replace("\n", " | "))
    tl = (m.get("metadata") or {}).get("timeline") or []
    print("timeline", len(tl))
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            continue
        kind = ev.get("kind")
        name = ev.get("toolName") or ""
        if kind == "tool":
            args = ev.get("arguments")
            arg_s = json.dumps(args, ensure_ascii=False)[:500] if args is not None else ""
            result = ev.get("result") or ev.get("output") or ev.get("content") or ""
            if not isinstance(result, str):
                result = json.dumps(result, ensure_ascii=False)
            err = ev.get("error")
            print(f"  [{j}] TOOL {name} step={ev.get('step')} phase={ev.get('phase')} err={err!r}")
            print(f"      args {arg_s}")
            print(f"      result {result[:400].replace(chr(10), ' ')}")
        elif kind == "model":
            print(f"  [{j}] model step={ev.get('step')} phase={ev.get('phase')}")
