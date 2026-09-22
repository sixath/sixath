import json
import urllib.request
from collections import Counter

def dump(sid):
    url = f"http://127.0.0.1:8000/api/v1/sessions/{sid}/messages"
    req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
    with urllib.request.urlopen(req, timeout=60) as r:
        data = json.loads(r.read().decode())
    items = data.get("items") or []
    print("====", sid, "n=", len(items))
    for i, m in enumerate(items):
        meta = m.get("metadata") or {}
        tl = meta.get("timeline") if isinstance(meta, dict) else []
        tools = Counter()
        skill_ev = []
        if isinstance(tl, list):
            for ev in tl:
                if not isinstance(ev, dict):
                    continue
                if ev.get("kind") == "tool":
                    name = ev.get("toolName") or "?"
                    tools[name] += 1
                    if name in ("load_skill", "skill_view", "read_skill_file", "skills_list"):
                        res = ev.get("result")
                        if isinstance(res, str):
                            rs = res
                        elif res is not None:
                            rs = json.dumps(res, ensure_ascii=False)
                        else:
                            rs = ""
                        skill_ev.append(
                            {
                                "step": ev.get("step"),
                                "name": name,
                                "phase": ev.get("phase"),
                                "args": ev.get("arguments"),
                                "result_len": len(rs),
                                "head": rs[:180].replace("\n", " "),
                            }
                        )
        print(
            f"  [{i}] {m.get('role')} id={(m.get('id') or '')[:8]} "
            f"clen={len(m.get('content') or '')} timeline={len(tl) if isinstance(tl, list) else tl}"
        )
        print("    tools", dict(tools))
        for s in skill_ev:
            print("    SKILL", s)


dump("3b15fb7b-c980-4356-be1b-43eb5078abfb")
dump("75f7b4da-a700-4b27-8be9-787aa1c895d5")
