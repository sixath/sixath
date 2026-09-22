import json
import urllib.request
from collections import Counter

sid = "f2c790d6-671d-499f-8b4e-0c53f86e4df4"
headers = {"Authorization": "Bearer dev-bootstrap-token"}

for base in ["http://127.0.0.1:8000", "http://10.86.32.78:8000"]:
    url = f"{base}/api/v1/sessions/{sid}/messages?limit=100"
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=20) as r:
            data = json.loads(r.read().decode())
        print("OK", base, "n=", len(data.get("items") or []))
        break
    except Exception as e:
        print("FAIL", base, e)
        data = None

if not data:
    raise SystemExit(1)

items = data.get("items") or []
for i, m in enumerate(items):
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") if isinstance(meta, dict) else []
    tools = Counter()
    phases = Counter()
    if isinstance(tl, list):
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            kind = ev.get("kind")
            phase = ev.get("phase")
            phases[f"{kind}:{phase}"] += 1
            if kind == "tool":
                tools[ev.get("toolName") or "?"] += 1
    print(
        f"\n[{i}] {m.get('role')} {m.get('createdAt')} id={(m.get('id') or '')[:8]} "
        f"clen={len(m.get('content') or '')} timeline={len(tl) if isinstance(tl, list) else tl}"
    )
    print("  content:", (m.get("content") or "")[:300].replace("\n", " | "))
    if isinstance(meta, dict):
        print("  meta keys", list(meta.keys())[:20])
        err = meta.get("error") or meta.get("stream_error") or meta.get("failed")
        if err:
            print("  ERROR", err)
    print("  phases", dict(phases))
    print("  tools", dict(tools))
    if isinstance(tl, list) and tl:
        last = tl[-8:]
        for ev in last:
            if not isinstance(ev, dict):
                continue
            print(
                "   ",
                ev.get("kind"),
                ev.get("phase"),
                ev.get("toolName") or ev.get("model"),
                "step",
                ev.get("step"),
                "err",
                (ev.get("error") or "")[:180],
            )
        # any interrupted
        for ev in tl:
            if isinstance(ev, dict) and ev.get("phase") == "interrupted":
                print("  INTERRUPTED", ev.get("kind"), ev.get("toolName") or ev.get("model"), "step", ev.get("step"))
