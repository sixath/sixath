import json

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\last_turn_summary.json", encoding="utf-8") as f:
    out = json.load(f)

# need full timeline with step numbers - refetch
import urllib.request
url = "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
data = json.load(urllib.request.urlopen(req, timeout=60))
tl = data["items"][-1]["metadata"]["timeline"]

tool_names = []
model_steps = []
for ev in tl:
    if ev.get("kind") == "tool":
        tool_names.append(ev.get("name") or (ev.get("tool") or {}).get("name") or "?")
        # name might be top-level - check
        if not tool_names[-1] or tool_names[-1] == "?":
            # from earlier dump, name wasn't in keys - look at id?
            pass
    if ev.get("kind") == "model":
        model_steps.append(ev.get("step"))

# get tool names properly from arguments/result structure
names = []
for ev in tl:
    if ev.get("kind") != "tool":
        continue
    # try common fields
    n = ev.get("name")
    if not n and isinstance(ev.get("tool"), dict):
        n = ev["tool"].get("name")
    # some timelines put name in phase payload
    if not n:
        # inspect keys
        pass
    names.append(n or "unknown")

print("tool count", len([e for e in tl if e.get("kind")=="tool"]))
print("model count", len([e for e in tl if e.get("kind")=="model"]))
print("model steps sample", model_steps[:5], "...", model_steps[-5:])
print("max model step", max([s for s in model_steps if isinstance(s,int)] or [0]))

# extract tool name from timeline - dump one tool event keys+partial
for ev in tl:
    if ev.get("kind")=="tool":
        print("sample tool keys", ev.keys())
        print("sample tool name field", ev.get("name"), ev.get("toolName"))
        # maybe name is only in UI from arguments - check result for tool identity
        args = ev.get("arguments")
        print("args type", type(args).__name__, str(args)[:200] if args else None)
        break

# Count by looking at decision or id prefix
from collections import Counter
ids = [ev.get("id","") for ev in tl if ev.get("kind")=="tool"]
print("id prefixes", Counter(i.split(":")[0] if i else "?" for i in ids))
# name might be embedded in id like terminal:xxx
print("id samples", ids[:3], ids[-3:])

# Check last model event fully
for ev in reversed(tl):
    if ev.get("kind")=="model":
        print("LAST MODEL:", json.dumps({k:ev.get(k) for k in ev}, ensure_ascii=False)[:800])
        break
for ev in reversed(tl):
    if ev.get("kind")=="tool":
        print("LAST TOOL id", ev.get("id"), "dur", ev.get("durationMs"), "phase", ev.get("phase"))
        print("LAST TOOL result head", str(ev.get("result"))[:300])
        break
