import json
from collections import Counter

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7.json", encoding="utf-8") as f:
    data = json.load(f)

items = data["items"]
user = items[0]
asst = items[1]
print("USER:", user.get("content"))
print("\nASST_CONTENT:\n", asst.get("content"))
print("\n--- timeline ---")
tl = asst["metadata"]["timeline"]
print("events", len(tl))

for ev in tl:
    kind = ev.get("kind")
    phase = ev.get("phase")
    step = ev.get("step")
    name = ev.get("toolName")
    extra = ""
    if kind == "tool":
        args = ev.get("arguments")
        if isinstance(args, str) and len(args) > 180:
            args = args[:180] + "..."
        err = ev.get("error")
        res = ev.get("result")
        if isinstance(res, str) and len(res) > 220:
            res = res[:220] + "..."
        extra = f" name={name} err={err} args={args} result={res}"
    elif kind == "model":
        extra = f" model={ev.get('model')} mode={ev.get('mode')} msgCount={ev.get('messageCount')} content={(ev.get('content') or ev.get('text') or ev.get('delta') or '')[:200]}"
        # dump remaining keys
        skip = {"kind", "phase", "step", "seq", "model", "mode", "messageCount", "id"}
        others = {k: ev.get(k) for k in ev if k not in skip}
        if others:
            compact = {}
            for k, v in others.items():
                s = str(v)
                compact[k] = s[:160] + ("..." if len(s) > 160 else "")
            extra += f" extras={compact}"
    print(f"[{ev.get('seq')}] step={step} {kind}/{phase}{extra}")
