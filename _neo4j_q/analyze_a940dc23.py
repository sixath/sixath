# -*- coding: utf-8 -*-
import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_a940dc23.json", encoding="utf-8"))
items = data.get("items") or []
m = items[-1]
print("ASSISTANT", m.get("createdAt"), "clen", len(m.get("content") or ""))
print("===== CONTENT =====")
print(m.get("content") or "")
print("===== END CONTENT =====\n")

tl = (m.get("metadata") or {}).get("timeline") or []
by = {}
for ev in tl:
    if not isinstance(ev, dict) or ev.get("kind") != "tool":
        continue
    by.setdefault(ev.get("id"), []).append(ev)

print(f"unique tools={len(by)}\n")
for i, (tid, evs) in enumerate(by.items(), 1):
    started = next((e for e in evs if e.get("phase") == "started"), None)
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    name = done.get("toolName")
    args = done.get("arguments") if done.get("arguments") is not None else (started or {}).get("arguments")
    result = done.get("result")
    err = done.get("error")
    print(f"\n==== {i:02d} {done.get('phase')} {name} err={err} ====")
    print("args", json.dumps(args, ensure_ascii=False)[:800])
    if not isinstance(result, dict):
        print("result", str(result)[:600])
        continue
    keys = list(result.keys())
    print("keys", keys)
    print("ok=", result.get("ok"), "error=", result.get("error"), "hit_status=", result.get("hit_status"))
    if name == "es_log_query":
        print("index=", result.get("index"), "queried_index=", result.get("queried_index"),
              "count=", result.get("count"), "total=", result.get("total"),
              "returned=", result.get("returned"), "from=", result.get("from"))
        hits = result.get("hits")
        print("hits type", type(hits).__name__, "n", len(hits) if isinstance(hits, list) else hits)
        if isinstance(hits, list):
            for hi, h in enumerate(hits[:3]):
                print(f"  HIT{hi}", json.dumps(h, ensure_ascii=False)[:500])
        refs = result.get("evidence_refs")
        if refs:
            print("evidence_refs n", len(refs), json.dumps(refs[:2], ensure_ascii=False)[:400])
    elif name in ("rca_grep",):
        matches = result.get("matches")
        print("matches n", len(matches) if isinstance(matches, list) else matches)
        if isinstance(matches, list):
            for mi, mm in enumerate(matches[:8]):
                print(f"  M{mi}", json.dumps(mm, ensure_ascii=False)[:350])
        refs = result.get("evidence_refs")
        if isinstance(refs, list):
            print("refs n", len(refs))
            for r in refs[:6]:
                print(" ", json.dumps(r, ensure_ascii=False)[:250])
    elif name in ("rca_read",):
        content = result.get("content") or result.get("text") or ""
        if isinstance(content, str):
            print("content_len", len(content))
            print(content[:700])
        else:
            print(json.dumps(result, ensure_ascii=False)[:700])
    elif name in ("execute_read", "list_tables", "describe_table"):
        print(json.dumps(result, ensure_ascii=False)[:900])
    else:
        print(json.dumps(result, ensure_ascii=False)[:500])
