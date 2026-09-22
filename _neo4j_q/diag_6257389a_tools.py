# -*- coding: utf-8 -*-
import json

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_6257389a.json"
out = r"E:\workspace\github\sixath\sixath\_neo4j_q\diag_6257389a_tools.txt"

with open(path, encoding="utf-8") as f:
    data = json.load(f)

items = data["items"]
focus = [11, 13, 17, 19, 21, 23]
lines = []

for i in focus:
    m = items[i]
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") or []
    lines.append("=" * 70)
    lines.append(f"msg[{i}] content_len={len(m.get('content') or '')} events={len(tl)}")
    lines.append(f"content={m.get('content')!r}")
    lines.append(f"meta_without_tl={json.dumps({k:v for k,v in meta.items() if k!='timeline'}, ensure_ascii=False)[:1500]}")
    for j, ev in enumerate(tl):
        lines.append(f"\n-- event[{j}] --")
        # full dump but truncate huge fields
        ev2 = json.loads(json.dumps(ev, ensure_ascii=False))
        for key in list(ev2.keys()):
            v = ev2[key]
            if isinstance(v, str) and len(v) > 800:
                ev2[key] = v[:400] + f"...<{len(v)} chars>..." + v[-200:]
            elif isinstance(v, dict):
                for k2, v2 in list(v.items()):
                    if isinstance(v2, str) and len(v2) > 800:
                        v[k2] = v2[:400] + f"...<{len(v2)} chars>..." + v2[-200:]
        lines.append(json.dumps(ev2, ensure_ascii=False, indent=2))

with open(out, "w", encoding="utf-8") as f:
    f.write("\n".join(lines))
print("wrote", out)
