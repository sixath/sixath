import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_26a9.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
for mi, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    print("=" * 60, "ASSISTANT", mi)
    tl = (m.get("metadata") or {}).get("timeline") or []
    for i, ev in enumerate(tl):
        if not isinstance(ev, dict) or ev.get("kind") != "tool":
            continue
        args = ev.get("arguments") or {}
        if isinstance(args, str):
            try:
                args = json.loads(args)
            except Exception:
                args = {}
        result = ev.get("result")
        if isinstance(result, str):
            try:
                result = json.loads(result)
            except Exception:
                result = {}
        if not isinstance(result, dict):
            result = {}
        hits = result.get("hits")
        nh = len(hits) if isinstance(hits, list) else -1
        q = args.get("query") if isinstance(args, dict) else ""
        if isinstance(q, str) and len(q) > 80:
            q = q[:80] + "..."
        print(
            f"step={ev.get('step')} {ev.get('toolName')} from={args.get('from')} limit={args.get('limit')} "
            f"res_from={result.get('from')} total={result.get('total')} hits={nh} trunc={result.get('truncated')} next={result.get('next_from')} "
            f"unknown={result.get('unknown_fields')} err={str(result.get('error') or '')[:80]!r}"
        )
        print(f"  q={q!r}")
