import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e9d4.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []

# inspect metadata keys and non-tool timeline kinds for each assistant
for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") if isinstance(meta, dict) else []
    print("=" * 80)
    print(f"ASST [{i}] meta_keys={list(meta.keys()) if isinstance(meta, dict) else None}")
    for k, v in meta.items() if isinstance(meta, dict) else []:
        if k == "timeline":
            continue
        s = json.dumps(v, ensure_ascii=False) if not isinstance(v, str) else v
        print(f"  {k}: {s[:500]}")
    kinds = {}
    if isinstance(tl, list):
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            k = f"{ev.get('kind')}/{ev.get('phase')}"
            kinds[k] = kinds.get(k, 0) + 1
            # print interesting non-tool events
            if ev.get("kind") not in ("tool", "model", None) or ev.get("phase") in (
                "retry", "inject", "nudge", "policy", "interrupted", "blocked",
            ):
                brief = {kk: ev.get(kk) for kk in ev if kk not in ("result",)}
                s = json.dumps(brief, ensure_ascii=False)
                print("  EV", s[:500])
            # check for inject-like fields
            for key in ev:
                if any(x in str(key).lower() for x in ("inject", "nudge", "retry", "gate", "policy", "drift", "claim", "credential")):
                    print("  FIELD", key, json.dumps(ev.get(key), ensure_ascii=False)[:300])
    print("  kinds", kinds)

# dump full content of last 4 assistant turns
print("\n\n######## FULL CONTENTS (last 6 assistants)")
asst = [m for m in items if m.get("role") == "assistant"]
for m in asst[-6:]:
    print("=" * 40)
    print(m.get("createdAt"), "clen", len(m.get("content") or ""))
    print(m.get("content") or "")
    print()
