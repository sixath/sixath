import json
import urllib.request

h = {"Authorization": "Bearer dev-bootstrap-token"}
sid = "75f7b4da-a700-4b27-8be9-787aa1c895d5"
url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages"
req = urllib.request.Request(url, headers=h)
with urllib.request.urlopen(req, timeout=60) as resp:
    data = json.load(resp)

items = data.get("items") or []
print("n", len(items))
for i, m in enumerate(items):
    md = m.get("metadata") or {}
    tl = md.get("timeline") if isinstance(md, dict) else None
    print("---")
    print("i", i)
    print("id", m.get("id"))
    print("role", m.get("role"))
    print("createdAt", m.get("createdAt"))
    print("content", (m.get("content") or "")[:200])
    print("content_len", len(m.get("content") or ""))
    print("metadata_type", type(md).__name__, "keys", list(md.keys())[:20] if isinstance(md, dict) else None)
    print("timeline_len", len(tl) if isinstance(tl, list) else tl)

# write slim
slim = []
for m in items:
    slim.append({
        "id": m.get("id"),
        "role": m.get("role"),
        "createdAt": m.get("createdAt"),
        "content": m.get("content"),
        "meta_keys": list((m.get("metadata") or {}).keys()) if isinstance(m.get("metadata"), dict) else m.get("metadata"),
        "timeline_n": len((m.get("metadata") or {}).get("timeline") or []) if isinstance(m.get("metadata"), dict) else 0,
    })
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_now.json", "w", encoding="utf-8") as f:
    json.dump(slim, f, ensure_ascii=False, indent=2)
print("wrote slim")
