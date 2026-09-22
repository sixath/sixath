import json, urllib.request
from collections import Counter

url = "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
data = json.load(urllib.request.urlopen(req, timeout=60))
tl = data["items"][-1]["metadata"]["timeline"]

names = Counter()
for ev in tl:
    if ev.get("kind") == "tool":
        names[ev.get("toolName") or "?"] += 1
print("tools used:", dict(names))
print("errors on tools:", sum(1 for ev in tl if ev.get("kind")=="tool" and ev.get("error")))
# any model with step -1
for ev in tl:
    if ev.get("kind")=="model" and ev.get("step") == -1:
        print("step-1 model:", {k:ev.get(k) for k in ev})
# check last few for error fields
for ev in tl[-5:]:
    print("tail", ev.get("kind"), ev.get("toolName"), ev.get("step"), ev.get("phase"), bool(ev.get("error")), str(ev.get("error"))[:80] if ev.get("error") else "")
