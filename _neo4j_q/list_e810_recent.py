# -*- coding: utf-8 -*-
import json
import sys
import urllib.request
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")
h = {"Authorization": "Bearer dev-bootstrap-token"}
aid = "e8107fb3-e40a-4207-9d9a-6768847aaf79"
base = "http://10.86.32.78:8000"

def get(url):
    req = urllib.request.Request(url, headers=h)
    with urllib.request.urlopen(req, timeout=60) as resp:
        return json.load(resp)

# try list endpoints
for url in [
    f"{base}/api/v1/agents/{aid}/sessions?limit=20",
    f"{base}/api/v1/sessions?agent_id={aid}&limit=20",
    f"{base}/runtime/v1/agents/{aid}/sessions",
]:
    try:
        data = get(url)
        print("OK", url, "keys", list(data.keys())[:12] if isinstance(data, dict) else type(data))
        items = data.get("items") or data.get("sessions") or []
        print("n", len(items))
        for it in items[:15]:
            print(" ", it.get("id") or it.get("sessionId"), it.get("updatedAt") or it.get("updated_at"), (it.get("title") or "")[:60], (it.get("lastMessage") or "")[:40] if isinstance(it.get("lastMessage"), str) else "")
        if items:
            break
    except Exception as e:
        print("FAIL", url, type(e).__name__, e)
