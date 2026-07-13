import json
import urllib.request

tool_id = "70f43be3-435a-49e8-997d-e3d2ae3d35bb"
agent_id = "e8107fb3-e40a-4207-9d9a-6768847aaf79"

t = json.load(urllib.request.urlopen(f"http://localhost:8000/api/v1/tools/{tool_id}"))
t["config"]["datasource"]["id"] = "cgarchive"
body = {
    "id": tool_id,
    "name": t["name"],
    "description": t.get("description", ""),
    "type": t["type"],
    "config": t["config"],
}
req = urllib.request.Request(
    f"http://localhost:8000/api/v1/tools/{tool_id}",
    data=json.dumps(body).encode(),
    headers={"Content-Type": "application/json"},
    method="PUT",
)
out = json.load(urllib.request.urlopen(req))
print("tool datasource id:", out["config"]["datasource"]["id"])

bind = json.dumps(
    {"id": agent_id, "tool_ids": [tool_id, "214bba80-f366-43cd-ad1c-6d4c76255c9d"]}
).encode()
req2 = urllib.request.Request(
    f"http://localhost:8000/api/v1/agents/{agent_id}/tools",
    data=bind,
    headers={"Content-Type": "application/json"},
    method="PUT",
)
try:
    urllib.request.urlopen(req2)
    print("agent tools: cgarchive first")
except Exception as e:
    print("bind tools:", e)
