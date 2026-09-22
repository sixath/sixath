# -*- coding: utf-8 -*-
import json, sys
sys.stdout.reconfigure(encoding="utf-8")
raw = open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_7a444288.json", encoding="utf-8").read()
needles = [
    "operation type not empty",
    "vm operation type",
    "UPSchedule",
    "ClearOperationType",
    "NoEmptyVmids",
    "rebootInstance",
    "reboot_instance",
    "rca_grep",
    "rca_read",
    "rca_glob",
    "search_files",
    "gitlab",
    "backend-cginstance",
    "backend-sched-hub",
    "由于上下文已压缩",
    "list_tools",
    "skills_list",
    "terminal",
]
for n in needles:
    print(f"{n!r}: {raw.count(n)}")

data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_7a444288.json", encoding="utf-8"))
for mi, m in enumerate(data["items"]):
    if m.get("role") != "assistant":
        continue
    tl = (m.get("metadata") or {}).get("timeline") or []
    models = []
    for ev in tl:
        if ev.get("kind") == "model":
            models.append((ev.get("phase"), ev.get("step"), ev.get("model"), ev.get("mode")))
    print(f"\nmsg{mi} models unique:", sorted(set(x[2] for x in models if x[2])))
    print("  first3", models[:3], "last3", models[-3:])
    kinds = {}
    for ev in tl:
        kinds[ev.get("kind")] = kinds.get(ev.get("kind"), 0) + 1
    print("  kinds", kinds)
    # look at huge es results that were strings
    for ev in tl:
        if ev.get("kind") != "tool" or ev.get("toolName") != "es_log_query" or ev.get("phase") != "completed":
            continue
        r = ev.get("result")
        if isinstance(r, str) and ("operation type" in r or "UPSchedule" in r or "sched-hub" in r[:2000]):
            print("  ES STRING contains keyword, len", len(r), "head", r[:200])
        if isinstance(r, dict):
            s = json.dumps(r, ensure_ascii=False)
            if "operation type" in s or "UPSchedule" in s:
                print("  ES DICT contains keyword count", s.count("operation type"), s.count("UPSchedule"), "len", len(s))
