import json, urllib.request, re

url = "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
data = json.load(urllib.request.urlopen(req, timeout=60))
items = data.get("items") or []
last = items[-1]
print("role", last.get("role"))
print("created", last.get("createdAt"))
content = last.get("content") or ""
print("content_len", len(content))
print("content_tail:", content[-500:])
print("---")
tl = (last.get("metadata") or {}).get("timeline")
print("timeline type", type(tl).__name__, "len", len(tl) if isinstance(tl, list) else "n/a")

if isinstance(tl, list):
    kinds = []
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        k = ev.get("kind") or ev.get("type") or ev.get("event") or "?"
        name = ""
        if isinstance(ev.get("tool"), dict):
            name = ev["tool"].get("name") or ""
        elif isinstance(ev.get("name"), str):
            name = ev.get("name")
        # common shapes
        for key in ("toolName", "tool_name", "name"):
            if not name and isinstance(ev.get(key), str):
                name = ev[key]
        kinds.append((k, name, list(ev.keys())[:8]))
    from collections import Counter
    print("event kinds", Counter(k for k, _, _ in kinds))
    print("last 15 events:")
    for row in kinds[-15:]:
        print(" ", row)
    # look for error/finish
    s = json.dumps(tl, ensure_ascii=False)
    for pat in ["error", "max_steps", "MaxSteps", "timeout", "canceled", "cancelled", "finish", "done", "skills_list", "load_skill"]:
        print(pat, len(re.findall(pat, s, flags=re.I)))

# dump compact timeline summary to file
out = {
    "content": content,
    "timeline_summary": [],
}
if isinstance(tl, list):
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        brief = {k: ev.get(k) for k in ("kind", "type", "event", "name", "toolName", "status", "error", "durationMs", "duration_ms") if k in ev}
        if "tool" in ev and isinstance(ev["tool"], dict):
            brief["tool_name"] = ev["tool"].get("name")
            brief["tool_ok"] = ev["tool"].get("ok")
        out["timeline_summary"].append(brief)

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\last_turn_summary.json", "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, indent=2)
print("wrote summary, timeline events", len(out["timeline_summary"]))
