# -*- coding: utf-8 -*-
import json, sys, urllib.request
sys.stdout.reconfigure(encoding="utf-8")
h = {"Authorization": "Bearer dev-bootstrap-token"}
base = "http://10.86.32.78:8000"

def get(url):
    req = urllib.request.Request(url, headers=h)
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)

for tid, label in [
    ("aa782fc7-97f8-4317-970c-0d568f38b48f", "zj-rca-code-search"),
    ("17fafca8-04a3-40d2-ad6d-03eee72a0ebd", "zj-elk"),
    ("da1d5fb1-8bcd-43da-9e9a-199252f0bcac", "yl-rca-symbol"),
    ("fab11ae1-7444-476d-92a1-ccff6592a547", "zj-elk_flow"),
]:
    t = get(f"{base}/api/v1/tools/{tid}")
    cfg = t.get("config") or {}
    print("\n====", label, t.get("name"), t.get("type"), "====")
    print(json.dumps(cfg, ensure_ascii=False, indent=2)[:2500])

# skills summary on agent?
ag = get(f"{base}/api/v1/agents/e8107fb3-e40a-4207-9d9a-6768847aaf79")
print("\nagent extra keys", [k for k in ag.keys() if k.lower() not in ("apikey","modelconfig")])
sp = ag.get("systemPrompt") or ag.get("system_prompt") or ""
print("systemPrompt len", len(sp))
print(sp[:1500])
