import json

d = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_4ff.json", encoding="utf-8"))
m = d["items"][9]
for ev in m["metadata"]["timeline"]:
    if ev.get("toolName") != "skill_manage":
        continue
    print("phase", ev.get("phase"), "error", ev.get("error"))
    print("args", json.dumps(ev.get("arguments"), ensure_ascii=False)[:200])
    print("result", json.dumps(ev.get("result"), ensure_ascii=False)[:800])
    print("---")
