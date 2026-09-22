import json, sys
sys.stdout.reconfigure(encoding="utf-8")
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8d647233.json", encoding="utf-8") as f:
    data = json.load(f)

# turn 1 execute_read of t_game_virtual_machine_info by flow_id
items = data["items"]
for mi, m in enumerate(items):
    tl = (m.get("metadata") or {}).get("timeline") or []
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            continue
        if ev.get("toolName") != "execute_read":
            continue
        args = ev.get("args") or {}
        q = str(args.get("query") or args.get("dsl") or "")
        if "t_game_virtual_machine_info" in q and "9999" in q:
            print("=" * 60)
            print("msg", mi, "ev", j)
            print("query", q)
            res = ev.get("result")
            print(json.dumps(res, ensure_ascii=False)[:4000] if res else "NO RESULT")
            print("error", ev.get("error"))
