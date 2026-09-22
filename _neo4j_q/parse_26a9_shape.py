import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_26a9.json", encoding="utf-8") as f:
    data = json.load(f)

m = (data.get("items") or [])[1]
tl = (m.get("metadata") or {}).get("timeline") or []
for i, ev in enumerate(tl):
    if not isinstance(ev, dict) or ev.get("kind") != "tool":
        continue
    result = ev.get("result")
    typ = type(result).__name__
    n = 0
    parsed = None
    parse_err = ""
    if isinstance(result, str):
        n = len(result)
        try:
            parsed = json.loads(result)
        except Exception as e:
            parse_err = str(e)[:80]
            # show tail
            print(f"[{i}] step={ev.get('step')} STR len={n} PARSE_FAIL {parse_err}")
            print("  head", result[:120].replace("\n"," "))
            print("  tail", result[-120:].replace("\n"," "))
            continue
    elif isinstance(result, dict):
        parsed = result
        n = len(json.dumps(result, ensure_ascii=False))
    else:
        print(f"[{i}] {typ}")
        continue
    hits = parsed.get("hits")
    nh = len(hits) if isinstance(hits, list) else type(hits).__name__
    print(
        f"[{i}] step={ev.get('step')} stored={typ} bytes={n} keys={list(parsed.keys())[:12]} "
        f"total={parsed.get('total')} from={parsed.get('from')} hits={nh} trunc={parsed.get('truncated')} next={parsed.get('next_from')}"
    )
    if isinstance(result, str) and not result.strip().endswith("}"):
        print("  WARN result string not closed")
