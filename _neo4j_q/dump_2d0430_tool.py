import json, sys, urllib.request

sys.stdout.reconfigure(encoding="utf-8")
sid = "2d0430f0-2369-43a3-9d64-b17a616159b7"
headers = {"Authorization": "Bearer dev-bootstrap-token"}

# tool id from agent
tid = "b83869c7-d7ee-4278-b62a-2a9943917bb8"
for path in [
    f"/api/v1/tools/{tid}",
    f"/api/v1/agents/1583f45b-d5c3-41ab-b1f7-1aa5539126d4",
]:
    url = "http://10.86.32.78:8000" + path
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=15) as r:
            data = json.loads(r.read().decode())
        print("===", path, "===")
        print(json.dumps(data, ensure_ascii=False)[:4000])
    except Exception as e:
        print("FAIL", path, e)

# try insights
url = "http://10.86.32.78:8000/api/v1/agents/1583f45b-d5c3-41ab-b1f7-1aa5539126d4/insights"
try:
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req, timeout=15) as r:
        print("insights", r.read()[:500])
except Exception as e:
    print("insights FAIL", e)
