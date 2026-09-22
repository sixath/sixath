import json
from collections import Counter

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_37ced.json"
with open(path, encoding="utf-8") as f:
    data = json.load(f)
items = data["items"]
m = items[3]
meta = m.get("metadata") or {}
tl = meta.get("timeline") or []
print("assistant[3] content_repr", repr(m.get("content")))
print("meta keys", list(meta.keys()))
print("timeline len", len(tl))

tools = Counter()
model_steps = []
errors = []
empty_results = []
for ev in tl:
    if ev.get("kind") == "tool":
        name = ev.get("toolName") or "?"
        tools[name] += 1
        res = ev.get("result")
        err = ev.get("error")
        if err:
            errors.append((ev.get("step"), name, str(err)[:300]))
        s = "" if res is None else str(res)
        if not err and (s.strip() in ("", "[]", "{}", "null", '""') or s.strip() in ('"[]"', "'[]'")):
            empty_results.append((ev.get("step"), name, s[:80], ev.get("arguments")))
        if name in ("execute_read", "describe_table", "list_tables", "load_skill", "rca_grep", "list_tools", "tool_search"):
            args = ev.get("arguments")
            print(f"TOOL step={ev.get('step')} {name} dur={ev.get('durationMs')} err={bool(err)} args={json.dumps(args, ensure_ascii=False)[:200]}")
            if err:
                print("  ERR", str(err)[:300])
            else:
                print("  RES", s[:250].replace("\n", " | "))
    elif ev.get("kind") == "model":
        model_steps.append({k: ev.get(k) for k in ("step", "phase", "mode", "model", "messageCount", "inputTokens", "outputTokens", "error")})
        if ev.get("error") or ev.get("phase") == "interrupted":
            print("MODEL", {k: ev.get(k) for k in ev})

print("\n=== model steps ===")
for x in model_steps:
    print(x)
print("\n=== tool counts ===", dict(tools))
print("=== errors ===", errors)
print("=== empty-ish ===", len(empty_results))
for x in empty_results[:20]:
    print(" empty", x[:3])

print("\n=== last 3 items roles ===")
for i, it in enumerate(items):
    print(i, it.get("role"), it.get("createdAt"), len(it.get("content") or ""), "id", it.get("id"))
