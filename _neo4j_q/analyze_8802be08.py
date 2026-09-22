import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8802be08.json", encoding="utf-8") as f:
    data = json.load(f)
m = data["items"][1]
print("created", m.get("createdAt"), "id", m.get("id"))
print("content:\n", m.get("content"))
tl = (m.get("metadata") or {}).get("timeline") or []
print("n events", len(tl))
for ev in tl:
    kind = ev.get("kind")
    if kind == "model":
        print(f"  MODEL step={ev.get('step')} phase={ev.get('phase')} seq={ev.get('seq')} mode={ev.get('mode')} in={ev.get('inputTokens')} out={ev.get('outputTokens')} msg={ev.get('messageCount')}")
    else:
        print(
            f"  TOOL  step={ev.get('step')} phase={ev.get('phase')} seq={ev.get('seq')} name={ev.get('toolName')} dur={ev.get('durationMs')} trunc={ev.get('truncated')} err={ev.get('error')}"
        )
        args = ev.get("arguments")
        if args:
            print("        args", json.dumps(args, ensure_ascii=False)[:220])
