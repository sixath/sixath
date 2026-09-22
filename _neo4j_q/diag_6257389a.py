import json
import urllib.request
from collections import Counter

sid = "6257389a-4712-4e17-9977-dd8f161592a8"
urls = [
    f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages",
    f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages",
]
data = None
for url in urls:
    try:
        req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
        data = json.load(urllib.request.urlopen(req, timeout=60))
        print("OK", url)
        break
    except Exception as e:
        print("FAIL", url, type(e).__name__, e)

if not data:
    raise SystemExit(1)

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_6257389a.json", "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)

items = data.get("items") or data.get("data") or data
if isinstance(items, dict):
    items = items.get("items") or []
print("messages", len(items))

for i, m in enumerate(items):
    role = m.get("role")
    content = m.get("content") or ""
    meta = m.get("metadata") or {}
    tl = meta.get("timeline")
    created = m.get("createdAt") or m.get("created_at")
    print(
        f"--- msg[{i}] role={role} created={created} "
        f"content_len={len(content)} timeline={len(tl) if isinstance(tl, list) else type(tl).__name__}"
    )
    if content:
        print("  head:", content[:180].replace("\n", " "))
        print("  tail:", content[-250:].replace("\n", " "))
    # look for interrupt signals in metadata
    for k in ("status", "error", "finishReason", "finish_reason", "canceled", "cancelled", "aborted"):
        if k in meta:
            print(f"  meta.{k}=", meta[k])
    if isinstance(tl, list) and tl:
        kinds = []
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            k = ev.get("kind") or ev.get("type") or ev.get("event") or "?"
            name = ""
            if isinstance(ev.get("tool"), dict):
                name = ev["tool"].get("name") or ""
            for key in ("toolName", "tool_name", "name"):
                if not name and isinstance(ev.get(key), str):
                    name = ev[key]
            err = ev.get("error") or ev.get("status")
            kinds.append((k, name, err))
        print("  event kinds:", Counter(k for k, _, _ in kinds))
        print("  last 20 events:")
        for row in kinds[-20:]:
            print("   ", row)
        s = json.dumps(tl, ensure_ascii=False)
        for pat in (
            "error",
            "max_steps",
            "MaxSteps",
            "timeout",
            "canceled",
            "cancelled",
            "abort",
            "interrupt",
            "interrupted",
            "finish",
            "done",
            "context",
            "budget",
            "token",
        ):
            import re

            n = len(re.findall(pat, s, flags=re.I))
            if n:
                print(f"  pat {pat}: {n}")
