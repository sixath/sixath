import json, urllib.request

url = "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
data = json.load(urllib.request.urlopen(req, timeout=60))
# message index 15 user at 20:34:58, 16 assistant
items = data["items"]
for i, m in enumerate(items):
    if (m.get("createdAt") or "").startswith("2026-08-16T20:3"):
        print(i, m.get("role"), m.get("createdAt"), (m.get("content") or "")[:80])

tl = items[16]["metadata"]["timeline"]
for ev in tl:
    if ev.get("kind") != "tool":
        continue
    name = ev.get("toolName")
    args = ev.get("arguments")
    err = ev.get("error")
    res = ev.get("result")
    print("---", name)
    print(" args:", json.dumps(args, ensure_ascii=False)[:300] if args else None)
    if err:
        print(" error:", str(err)[:300])
    if name in ("load_skill", "skills_list", "rca_grep", "rca_glob", "rca_read", "read_file", "search_files"):
        print(" result:", str(res)[:400])
