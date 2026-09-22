import json, urllib.request, re
from collections import Counter

h = {"Authorization": "Bearer dev-bootstrap-token"}
msgs = json.load(
    urllib.request.urlopen(
        urllib.request.Request(
            "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages",
            headers=h,
        )
    )
)
items = msgs.get("items") or []
print("n", len(items))
for i, m in enumerate(items[-8:]):
    idx = len(items) - 8 + i
    print(idx, m.get("role"), m.get("createdAt"), (m.get("content") or "")[:80].replace("\n", " "))

found = False
for m in reversed(items):
    tl = (m.get("metadata") or {}).get("timeline") or []
    for ev in tl:
        if ev.get("toolName") != "list_tools":
            continue
        found = True
        s = json.dumps(ev.get("result"), ensure_ascii=False)
        print("list_tools at", m.get("createdAt"), "chars", len(s))
        for name in [
            "rca_grep",
            "rca_glob",
            "rca_read",
            "ssh_exec",
            "terminal",
            "search_files",
            "read_file",
            "list_tools",
        ]:
            print(" ", name, s.count(name))
        names = sorted(set(re.findall(r'"name"\s*:\s*"([^"]+)"', s)))
        print("names:", names)
        groups = list(re.findall(r'"([a-zA-Z_]+)"\s*:\s*\[', s))
        print("group-ish keys sample", groups[:20])
        break
    if found:
        break

# latest assistant tool mix if any after 21:08
for m in reversed(items):
    if m.get("role") != "assistant":
        continue
    tl = (m.get("metadata") or {}).get("timeline") or []
    if not tl:
        continue
    c = Counter(ev.get("toolName") for ev in tl if ev.get("kind") == "tool")
    print("asst", m.get("createdAt"), "tools", dict(c))
    break
