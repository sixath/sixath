import json
import sys
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_7a444288.json", encoding="utf-8"))
items = data["items"]

for mi, m in enumerate(items):
    if m.get("role") != "assistant":
        print(f"\n===== MSG {mi} USER =====")
        print(m.get("content"))
        continue
    print(f"\n===== MSG {mi} ASSISTANT {m.get('createdAt')} =====")
    content = m.get("content") or ""
    print("CONTENT LEN", len(content))
    print("CONTENT:\n", content)
    print("----- END CONTENT -----")
    tl = (m.get("metadata") or {}).get("timeline") or []
    print("timeline events", len(tl))
    kinds = Counter()
    for ev in tl:
        if isinstance(ev, dict):
            kinds[f"{ev.get('kind')}:{ev.get('phase')}"] += 1
    print("kinds", dict(kinds))

    by_id = {}
    for ev in tl:
        if not isinstance(ev, dict) or ev.get("kind") != "tool":
            continue
        by_id.setdefault(ev.get("id"), []).append(ev)
    print("unique tool calls", len(by_id))

    name_c = Counter()
    for tid, evs in by_id.items():
        done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
        name = done.get("toolName") or "?"
        name_c[f"{name}:{done.get('phase')}"] += 1
    print("tool names", dict(name_c))

    print("\n==== TOOL SEQUENCE ====")
    for i, (tid, evs) in enumerate(by_id.items(), 1):
        started = next((e for e in evs if e.get("phase") == "started"), None)
        done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
        name = done.get("toolName") or (started or {}).get("toolName")
        args = done.get("arguments")
        if args is None and started:
            args = started.get("arguments")
        result = done.get("result")
        err = done.get("error")
        dur = done.get("durationMs") or done.get("duration_ms")
        arg_s = json.dumps(args, ensure_ascii=False) if args is not None else ""
        if len(arg_s) > 400:
            arg_s = arg_s[:400] + "..."
        rsum = ""
        if isinstance(result, dict):
            keys = list(result.keys())[:16]
            rsum = "keys=" + ",".join(str(k) for k in keys)
            for k in ("ok", "error", "error_code", "file", "action", "status", "statusCode"):
                if k in result:
                    rsum += f" {k}={result.get(k)}"
            if "matches" in result:
                mm = result["matches"]
                rsum += f" matches={len(mm) if isinstance(mm, list) else type(mm).__name__}"
            if "content" in result and isinstance(result["content"], str):
                rsum += f" content_len={len(result['content'])} head={result['content'][:180]!r}"
            if "Rows" in result:
                rows = result["Rows"]
                rsum += f" rows={len(rows) if isinstance(rows, list) else type(rows).__name__}"
                if isinstance(rows, list) and rows:
                    rsum += " row0=" + json.dumps(rows[0], ensure_ascii=False)[:220]
            if "hits" in result:
                hits = result["hits"]
                rsum += f" hits={len(hits) if isinstance(hits, list) else type(hits).__name__}"
            body = result.get("body") or result.get("text") or result.get("output")
            if isinstance(body, str) and body:
                rsum += f" body_len={len(body)} head={body[:200]!r}"
            elif isinstance(body, (dict, list)):
                rsum += " body=" + json.dumps(body, ensure_ascii=False)[:200]
        elif isinstance(result, str):
            rsum = f"str_len={len(result)} head={result[:220]!r}"
        elif result is not None:
            rsum = str(result)[:220]
        print(f"\n{i:03d} step={done.get('step')} {done.get('phase')} {name} {dur}ms err={err}")
        print(f"     args: {arg_s}")
        print(f"     result: {rsum[:500]}")

    print("\n==== MODEL NODES ====")
    for ev in tl:
        if ev.get("kind") == "model":
            print(
                ev.get("phase"),
                "step",
                ev.get("step"),
                "mode",
                ev.get("mode"),
                "model",
                ev.get("model"),
                "in",
                ev.get("inputTokens") or ev.get("input_tokens"),
                "out",
                ev.get("outputTokens") or ev.get("output_tokens"),
                "finish",
                ev.get("finishReason") or ev.get("finish_reason"),
            )
