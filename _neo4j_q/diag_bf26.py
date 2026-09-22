import json
import urllib.request

sid = "bf26ea59-9116-41fc-91a7-8ffbe7a34919"
urls = [
    f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages",
    f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages",
    f"http://10.86.32.78:5173/api/v1/sessions/{sid}/messages",
]
out = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_bf26.json"

for url in urls:
    print("TRY", url)
    req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            data = json.load(resp)
    except Exception as e:
        print("  ERR", type(e).__name__, e)
        continue

    items = data.get("items") or []
    print("  OK messages", len(items), "keys", list(data.keys())[:10])
    with open(out, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False)
    print("  wrote", out)
    for i, m in enumerate(items):
        role = m.get("role")
        content = (m.get("content") or "").replace("\n", " ")
        created = m.get("createdAt")
        print(f"  [{i}] {role} {created} len={len(m.get('content') or '')} {content[:240]!r}")
        md = m.get("metadata") or {}
        tl = md.get("timeline") or []
        print("      metadata_keys", list(md.keys())[:30], "timeline", len(tl) if isinstance(tl, list) else type(tl))
    break
