# -*- coding: utf-8 -*-
import json
import sys
import urllib.request

sys.stdout.reconfigure(encoding="utf-8")
sid = "a940dc23-4db4-4726-b11d-2b9b49500159"
h = {"Authorization": "Bearer dev-bootstrap-token"}
url = f"http://10.86.32.78:8000/api/v1/sessions/{sid}/messages?limit=50"
req = urllib.request.Request(url, headers=h)
data = json.loads(urllib.request.urlopen(req, timeout=60).read().decode())
out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_a940dc23.json"
with open(out_path, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False)
print("wrote", out_path, "n=", len(data.get("items") or []))

items = data.get("items") or []
for i, m in enumerate(items):
    print(f"[{i}] {m.get('role')} {m.get('createdAt')} clen={len(m.get('content') or '')}")
    print((m.get("content") or "")[:400])
    tl = (m.get("metadata") or {}).get("timeline") or []
    print(" timeline", len(tl) if isinstance(tl, list) else type(tl))
    if not isinstance(tl, list):
        continue
    by = {}
    for ev in tl:
        if not isinstance(ev, dict) or ev.get("kind") != "tool":
            continue
        by.setdefault(ev.get("id"), []).append(ev)
    for j, (tid, evs) in enumerate(by.items(), 1):
        done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
        started = next((e for e in evs if e.get("phase") == "started"), None)
        name = done.get("toolName")
        args = done.get("arguments") if done.get("arguments") is not None else (started or {}).get("arguments")
        result = done.get("result")
        err = done.get("error")
        print(f"  {j:02d} {done.get('phase')} {name} err={err}")
        print("     args", json.dumps(args, ensure_ascii=False)[:500])
        if isinstance(result, dict):
            print("     result keys", list(result.keys())[:20], "ok=", result.get("ok"), "error=", str(result.get("error"))[:300])
        else:
            print("     result", str(result)[:400])
