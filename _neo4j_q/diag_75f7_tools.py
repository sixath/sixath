import json

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_list_tools_step6.json", encoding="utf-8") as f:
    data = json.load(f)

s = data["result"]
if isinstance(s, str):
    print("result type str len", len(s))
    print("truncated marker", "truncated" in s or "…" in s)
    for name in [
        "http_request", "es_log_query", "jaeger_trace", "rca_read", "rca_grep",
        "execute_read", "execute_write", "todo", "load_skill", "web_search",
        "memory_recall", "knowledge_search",
    ]:
        print(f"  {name}: {s.count(name)}")
    print("tail:", s[-400:])

# also check timeline truncation of ES - look at first log messages from the truncated JSON
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7.json", encoding="utf-8") as f:
    sess = json.load(f)
tl = sess["items"][1]["metadata"]["timeline"]
ev = None
for e in tl:
    if e.get("toolName") == "http_request" and e.get("phase") == "completed" and e.get("step") == 4:
        ev = e
        break
res = ev.get("result")
print("\nES result type", type(res).__name__)
if isinstance(res, str):
    print("len", len(res), "ends with", repr(res[-80:]))
    # extract M fields with regex
    import re
    ms = re.findall(r'"M"\s*:\s*"(.*?)"\s*,', res)
    print("M field count in preview", len(ms))
    for m in ms[:8]:
        print(" M:", m[:200])
    # also L field
    ls = re.findall(r'"L"\s*:\s*"?(.*?)"?\s*,', res)
    print("sample L", ls[:10])
    idxs = re.findall(r'"_index"\s*:\s*"([^"]+)"', res)
    print("indexes in preview", idxs[:20], "n=", len(idxs))

# duration of run
print("\nuser created", sess["items"][0].get("createdAt"))
print("asst created", sess["items"][1].get("createdAt"))

# durationMs of tools
total_ms = 0
for e in tl:
    if e.get("kind") == "tool":
        d = e.get("durationMs") or 0
        total_ms += d
        if d:
            print(" tool", e.get("step"), e.get("toolName"), e.get("phase"), "dur", d)
print("sum durationMs", total_ms)

# check if result has truncated suffix
print("\nresult suffix truncated?", str(res)[-60:] if res else None)
