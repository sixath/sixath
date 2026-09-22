import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_26a9.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
for mi, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    print("=" * 80)
    print(f"ASSISTANT [{mi}] {m.get('createdAt')} content_len={len(m.get('content') or '')}")
    print((m.get("content") or "")[:1500])
    print("--- timeline ---")
    tl = (m.get("metadata") or {}).get("timeline") or []
    for i, ev in enumerate(tl):
        if not isinstance(ev, dict):
            continue
        kind = ev.get("kind")
        if kind == "model":
            print(f"  [{i}] model {ev.get('phase')} {ev.get('model')} step={ev.get('step')} msgs={ev.get('messageCount')}")
            continue
        if kind != "tool":
            print(f"  [{i}] {kind} { {k: ev.get(k) for k in ev if k in ('phase','reason','error','name')} }")
            continue
        args = ev.get("arguments") or ev.get("input") or {}
        if isinstance(args, str):
            try:
                args = json.loads(args)
            except Exception:
                pass
        result = ev.get("result")
        rsum = ""
        if isinstance(result, str):
            rsum = result[:400]
            try:
                rj = json.loads(result)
                result = rj
            except Exception:
                pass
        extra = {}
        if isinstance(result, dict):
            extra = {
                "total": result.get("total"),
                "from": result.get("from"),
                "truncated": result.get("truncated"),
                "next_from": result.get("next_from"),
                "hits": len(result.get("hits") or []) if isinstance(result.get("hits"), list) else None,
                "unknown_fields": result.get("unknown_fields"),
                "query_rewritten": result.get("query_rewritten"),
                "ok": result.get("ok"),
                "error": str(result.get("error") or "")[:120],
                "hit_status": result.get("hit_status"),
            }
            rsum = json.dumps({k: extra[k] for k in extra if extra[k] is not None}, ensure_ascii=False)
        arg_brief = args
        if isinstance(args, dict):
            qb = args.get("query")
            if isinstance(qb, str) and len(qb) > 180:
                qb = qb[:180] + "..."
            arg_brief = {
                "index": args.get("index"),
                "from": args.get("from"),
                "limit": args.get("limit"),
                "query": qb,
                "trace_id": args.get("trace_id"),
            }
        print(f"  [{i}] {ev.get('toolName')} {ev.get('phase')} {ev.get('durationMs')}ms step={ev.get('step')}")
        print(f"       args {arg_brief}")
        print(f"       res  {rsum}")
        if ev.get("error"):
            print(f"       ERR {ev.get('error')}")
