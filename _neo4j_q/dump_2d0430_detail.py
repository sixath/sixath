import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_2d0430.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
m = items[1]
meta = m.get("metadata") or {}
tl = meta.get("timeline") or []
print("timeline events", len(tl))
print("assistant content:\n", m.get("content"))
print("\n===== FULL TIMELINE =====")
for i, ev in enumerate(tl):
    if not isinstance(ev, dict):
        print(i, ev)
        continue
    kind = ev.get("kind")
    step = ev.get("step")
    phase = ev.get("phase")
    extra = []
    for k in sorted(ev.keys()):
        if k in ("kind", "step", "phase", "seq"):
            continue
        v = ev.get(k)
        if k in ("result", "arguments", "content", "text", "thought", "prompt", "delta"):
            s = json.dumps(v, ensure_ascii=False) if not isinstance(v, str) else v
            extra.append(f"{k}={s[:500]}")
        else:
            extra.append(f"{k}={v}")
    print(f"[{i}] kind={kind} step={step} phase={phase} seq={ev.get('seq')} | " + " | ".join(extra)[:1500])
