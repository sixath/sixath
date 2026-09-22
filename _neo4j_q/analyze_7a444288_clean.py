# -*- coding: utf-8 -*-
import json
from collections import Counter

data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_7a444288.json", encoding="utf-8"))
out = open(r"E:\workspace\github\sixath\sixath\_neo4j_q\analyze_7a444288_clean.txt", "w", encoding="utf-8")

def p(*a):
    print(*a, file=out)

items = data["items"]
for mi, m in enumerate(items):
    p(f"\n===== MSG {mi} {m.get('role')} {m.get('createdAt')} =====")
    p(m.get("content") or "")

asst = items[3]
tl = asst["metadata"]["timeline"]
by_id = {}
for ev in tl:
    if isinstance(ev, dict) and ev.get("kind") == "tool":
        by_id.setdefault(ev.get("id"), []).append(ev)

def done_of(evs):
    return next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])

def parse_result(done):
    result = done.get("result")
    if isinstance(result, str):
        try:
            result = json.loads(result)
        except Exception:
            return {"_raw": result[:400]}
    return result if isinstance(result, dict) else {"_val": result}

p("\n\n===== TOOL NAME COUNTS =====")
c = Counter()
for evs in by_id.values():
    d = done_of(evs)
    c[f"{d.get('toolName')}:{d.get('phase')}"] += 1
p(dict(c))

p("\n===== ALL ES QUERIES (compact) =====")
keywords = [
    "operation type", "UPSchedule", "operation_type", "ClearOperationType",
    "cginstance", "sched-hub", "cgschedule", "NoEmpty", "rebootInstance",
    "backend-sched", "backend-cginstance", "reboot_instance",
]
for i, evs in enumerate(by_id.values(), 1):
    d = done_of(evs)
    if d.get("toolName") != "es_log_query":
        continue
    args = d.get("arguments") or {}
    r = parse_result(d)
    hits = r.get("hits")
    n = len(hits) if isinstance(hits, list) else None
    p(f"\n[{i}] step={d.get('step')} cluster={args.get('cluster')} index={args.get('index')!r} limit={args.get('limit')}")
    p(f"    query={args.get('query')}")
    p(f"    ok={r.get('ok')} err={r.get('error')} count={r.get('count')} hits_n={n} qindex={r.get('queried_index')}")
    if isinstance(hits, list) and hits:
        h0 = hits[0]
        src = h0.get("_source") or h0.get("source") or h0
        if isinstance(src, dict):
            p(f"    hit0 LAPP={src.get('LAPP')} LFILE={src.get('LFILE')} HOST={src.get('HOST')}")
            msg = src.get("M") or src.get("message") or src.get("msg") or ""
            p(f"    hit0 msg={str(msg)[:350]}")

p("\n===== KEYWORD PRESENCE IN ALL TOOL ARGS/RESULTS =====")
blob = json.dumps(tl, ensure_ascii=False)
for k in keywords + ["需重启", "操作不合法", "gitlab", "rca_grep", "rca_read", "search_files"]:
    p(f"  {k!r}: {blob.count(k)}")

p("\n===== SEARCH_FILES FULL =====")
for i, evs in enumerate(by_id.values(), 1):
    d = done_of(evs)
    if d.get("toolName") != "search_files":
        continue
    p(f"\n[{i}] step={d.get('step')} args={json.dumps(d.get('arguments'), ensure_ascii=False)}")
    r = parse_result(d)
    p(f"    result_keys={list(r.keys())[:30]}")
    p(json.dumps({k: r.get(k) for k in r if k in ('ok','error','count','total','matches','files') or k.endswith('_count')}, ensure_ascii=False)[:2000])
    ms = r.get("matches") or r.get("files") or r.get("results")
    if isinstance(ms, list):
        p(f"    n={len(ms)}")
        for m in ms[:15]:
            p("   ", json.dumps(m, ensure_ascii=False)[:400])

p("\n===== LOAD_SKILL NAMES + FORBID LINES =====")
for i, evs in enumerate(by_id.values(), 1):
    d = done_of(evs)
    if d.get("toolName") != "load_skill":
        continue
    args = d.get("arguments") or {}
    text = d.get("result")
    if not isinstance(text, str):
        text = json.dumps(text, ensure_ascii=False)
    p(f"\n[{i}] step={d.get('step')} name={args.get('name')} len={len(text)}")
    for line in text.splitlines():
        if any(x in line for x in ["禁止", "必须", "先", "源码", "实例", "ES", "gitlab", "不要", "立刻"]):
            p("   ", line[:240])

p("\n===== HTTP URL SUMMARY =====")
for i, evs in enumerate(by_id.values(), 1):
    d = done_of(evs)
    if d.get("toolName") != "http_request":
        continue
    args = d.get("arguments") or {}
    url = args.get("url") or ""
    body = args.get("body") or ""
    r = parse_result(d)
    p(f"[{i}] step={d.get('step')} {d.get('phase')} {d.get('durationMs')}ms status={r.get('status')} {r.get('status_code')}")
    p(f"    url={url[:180]}")
    if body:
        p(f"    body={str(body)[:180]}")
    b = r.get("body")
    if isinstance(b, str):
        p(f"    resp={b[:180]}")

p("\n===== MODEL INTERRUPT / COMPRESSION =====")
for ev in tl:
    if ev.get("kind") == "model":
        fr = ev.get("finishReason") or ev.get("finish_reason")
        if ev.get("phase") in ("interrupted", "error") or fr:
            p(ev.get("phase"), "step", ev.get("step"), "model", ev.get("model"), "finish", fr)
    if ev.get("kind") in ("context", "compression", "system"):
        p("CTX", json.dumps(ev, ensure_ascii=False)[:500])

p("\n===== FIRST TURN TOOLS =====")
tl1 = (items[1].get("metadata") or {}).get("timeline") or []
by1 = {}
for ev in tl1:
    if isinstance(ev, dict) and ev.get("kind") == "tool":
        by1.setdefault(ev.get("id"), []).append(ev)
for i, evs in enumerate(by1.values(), 1):
    d = done_of(evs)
    p(f"[{i}] {d.get('toolName')} step={d.get('step')} args={json.dumps(d.get('arguments'), ensure_ascii=False)[:300]}")
    r = parse_result(d)
    if d.get("toolName") == "es_log_query":
        p(f"    ok={r.get('ok')} err={r.get('error')} count={r.get('count')} hits={len(r.get('hits') or []) if isinstance(r.get('hits'), list) else r.get('hits')}")

out.close()
print("wrote")
