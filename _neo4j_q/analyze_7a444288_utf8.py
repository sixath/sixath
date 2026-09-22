# -*- coding: utf-8 -*-
import json
import sys
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_7a444288.json", encoding="utf-8"))
items = data["items"]
asst = items[3]
tl = asst["metadata"]["timeline"]

print("===== FINAL CONTENT =====")
print(asst.get("content") or "")
print("===== END CONTENT =====\n")

by_id = {}
for ev in tl:
    if not isinstance(ev, dict) or ev.get("kind") != "tool":
        continue
    by_id.setdefault(ev.get("id"), []).append(ev)

print("==== ES QUERIES ====")
for i, (tid, evs) in enumerate(by_id.items(), 1):
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    if done.get("toolName") != "es_log_query":
        continue
    args = done.get("arguments") or {}
    result = done.get("result")
    if isinstance(result, str):
        try:
            result = json.loads(result)
        except Exception:
            result = {"raw": result[:300]}
    ok = result.get("ok") if isinstance(result, dict) else None
    err = result.get("error") if isinstance(result, dict) else None
    hits = result.get("hits") if isinstance(result, dict) else None
    count = result.get("count") or result.get("total") if isinstance(result, dict) else None
    index = result.get("queried_index") if isinstance(result, dict) else None
    print(f"\nES#{i} step={done.get('step')} {done.get('phase')} {done.get('durationMs')}ms")
    print("  args", json.dumps(args, ensure_ascii=False)[:500])
    print(f"  ok={ok} err={err} count={count} queried_index={index} hits_n={len(hits) if isinstance(hits, list) else hits}")
    if isinstance(hits, list) and hits:
        for h in hits[:3]:
            if isinstance(h, dict):
                src = h.get("_source") or h.get("source") or h
                # compact interesting fields
                interesting = {}
                for k in ("M", "message", "msg", "LFILE", "LAPP", "HOST", "@timestamp", "T"):
                    if isinstance(src, dict) and k in src:
                        interesting[k] = str(src[k])[:240]
                if not interesting:
                    interesting = {"keys": list(src.keys())[:20] if isinstance(src, dict) else type(src).__name__, "head": json.dumps(src, ensure_ascii=False)[:300]}
                print("  HIT", json.dumps(interesting, ensure_ascii=False)[:500])

print("\n==== SEARCH_FILES ====")
for i, (tid, evs) in enumerate(by_id.items(), 1):
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    if done.get("toolName") != "search_files":
        continue
    args = done.get("arguments") or {}
    result = done.get("result")
    if isinstance(result, str):
        try:
            result = json.loads(result)
        except Exception:
            pass
    print(f"\nSF#{i} step={done.get('step')}")
    print("  args", json.dumps(args, ensure_ascii=False)[:600])
    if isinstance(result, dict):
        print("  keys", list(result.keys())[:20])
        print("  summary", json.dumps({k: result.get(k) for k in ("ok", "error", "count", "matches") if k in result}, ensure_ascii=False)[:800])
        ms = result.get("matches")
        if isinstance(ms, list):
            print("  matches_n", len(ms))
            for m in ms[:8]:
                print("   ", json.dumps(m, ensure_ascii=False)[:250])
    else:
        print("  result", str(result)[:400])

print("\n==== LOAD_SKILL ====")
for i, (tid, evs) in enumerate(by_id.items(), 1):
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    if done.get("toolName") != "load_skill":
        continue
    args = done.get("arguments") or {}
    result = done.get("result")
    text = result if isinstance(result, str) else json.dumps(result, ensure_ascii=False)
    print(f"\nSKILL#{i} step={done.get('step')} name={args.get('name')} len={len(text)}")
    print(text[:1800])
    print("-----")

print("\n==== SQL ====")
for i, (tid, evs) in enumerate(by_id.items(), 1):
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    if done.get("toolName") not in ("execute_read", "describe_table", "list_tables"):
        continue
    args = done.get("arguments") or {}
    result = done.get("result")
    print(f"\nSQL#{i} {done.get('toolName')} step={done.get('step')}")
    print("  args", json.dumps(args, ensure_ascii=False)[:400])
    if isinstance(result, dict):
        print("  rows", result.get("Rows"))
        print("  cols", result.get("Columns") or result.get("columns"))

print("\n==== HTTP FAILED / INTERESTING ====")
for i, (tid, evs) in enumerate(by_id.items(), 1):
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    if done.get("toolName") != "http_request":
        continue
    if done.get("phase") == "failed" or (isinstance(done.get("result"), dict) and (done["result"].get("status_code") not in (200, None))):
        print(i, done.get("phase"), done.get("error"), json.dumps(done.get("arguments"), ensure_ascii=False)[:200], str(done.get("result"))[:200])

print("\n==== COMPRESSION / MODEL HINTS ====")
for ev in tl:
    if ev.get("kind") in ("context", "compression", "memory"):
        print(ev)
    blob = json.dumps(ev, ensure_ascii=False)
    if any(x in blob for x in ["压缩", "compress", "L0", "L2", "budget", "snip"]):
        if ev.get("kind") != "tool":
            print("HINT", ev.get("kind"), ev.get("phase"), blob[:300])

print("\n==== FIRST TURN CONTENT =====")
print(items[1].get("content"))
