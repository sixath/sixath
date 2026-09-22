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
print("n", len(items))
m = items[-1]
print("last", m.get("role"), m.get("createdAt"), "clen", len(m.get("content") or ""))
print((m.get("content") or "")[:500].replace("\n", " | "))
tl = (m.get("metadata") or {}).get("timeline") or []
print("tl", len(tl))

n_http = empty = nonempty = fail = 0
urls = set()
for ev in tl:
    if not isinstance(ev, dict) or ev.get("kind") != "tool":
        continue
    name = ev.get("toolName")
    if name != "http_request":
        print("TOOL", name, ev.get("phase"), ev.get("error"))
        continue
    n_http += 1
    args = ev.get("arguments") or {}
    urls.add(str(args.get("url", ""))[:90])
    cmd = str(args.get("body", "")).replace("\n", " ")[:180]
    err = ev.get("error")
    result = ev.get("result") or ""
    if not isinstance(result, str):
        result = json.dumps(result, ensure_ascii=False)
    inner = ""
    try:
        outer = json.loads(result) if result.strip().startswith("{") else {}
        inner_raw = outer.get("body", "")
        if isinstance(inner_raw, str) and inner_raw.strip().startswith("{"):
            innerj = json.loads(inner_raw)
            inner = innerj.get("body", "") if isinstance(innerj, dict) else inner_raw
        else:
            inner = str(inner_raw)
    except Exception:
        inner = result[:120]
    inner_s = str(inner).strip()
    if err:
        fail += 1
        st = "FAIL"
    elif not inner_s:
        empty += 1
        st = "EMPTY"
    else:
        nonempty += 1
        st = "HIT"
    print(f"{n_http:02d} {st} {args.get('method')} {str(args.get('url'))[:75]}")
    print("   cmd", cmd)
    if st == "HIT":
        print("   inner", inner_s[:220].replace("\n", " "))
    elif st == "FAIL":
        print("   err", str(err)[:180])

print("summary http", n_http, "empty", empty, "hit", nonempty, "fail", fail)
print("urls", urls)
