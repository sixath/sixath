import json
import urllib.request

h = {"Authorization": "Bearer dev-bootstrap-token"}
ids = [
    "9ab2b157-b882-4924-8096-127cf2a276b6",  # zj-jaeger
    "17fafca8-04a3-40d2-ad6d-03eee72a0ebd",  # zj-elk
    "aa782fc7-97f8-4317-970c-0d568f38b48f",  # zj-rca-code-search
    "da1d5fb1-8bcd-43da-9e9a-199252f0bcac",  # yl-rca-symbol
]

def get(url):
    req = urllib.request.Request(url, headers=h)
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)

for tid in ids:
    t = get(f"http://127.0.0.1:8000/api/v1/tools/{tid}")
    print("=" * 60)
    print(t.get("name"), t.get("type"))
    print(json.dumps(t.get("config"), ensure_ascii=False, indent=2))
