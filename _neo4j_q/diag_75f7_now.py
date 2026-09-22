import json
import urllib.request
from datetime import datetime

h = {"Authorization": "Bearer dev-bootstrap-token"}
sid = "75f7b4da-a700-4b27-8be9-787aa1c895d5"
aid = "e8107fb3-e40a-4207-9d9a-6768847aaf79"

def get(url):
    req = urllib.request.Request(url, headers=h)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, json.load(resp)
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        print("HTTP", e.code, url, body[:500])
        return e.code, None
    except Exception as e:
        print("ERR", type(e).__name__, e, url)
        return 0, None

print("=== session ===")
st, sess = get(f"http://127.0.0.1:8000/api/v1/sessions/{sid}")
if sess:
    print(json.dumps({k: sess.get(k) for k in list(sess)[:30]}, ensure_ascii=False)[:2000])

print("\n=== messages ===")
st, data = get(f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages")
if data:
    items = data.get("items") or []
    print("n", len(items), "keys", list(data.keys()))
    for i, m in enumerate(items):
        c = (m.get("content") or "").replace("\n", " ")
        print(f"[{i}] {m.get('role')} {m.get('createdAt')} len={len(m.get('content') or '')} {c[:120]}")

print("\n=== agent ===")
st, ag = get(f"http://127.0.0.1:8000/api/v1/agents/{aid}")
if ag:
    print("name", ag.get("name"), "id", ag.get("id"))
