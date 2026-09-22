import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8555_full.json", encoding="utf-8"))
asst = [it for it in data["items"] if it.get("role") == "assistant"][-1]
tl = asst["metadata"]["timeline"]
print("timeline events", len(tl))

by_id = {}
for ev in tl:
    if not isinstance(ev, dict) or ev.get("kind") != "tool":
        continue
    by_id.setdefault(ev.get("id"), []).append(ev)

print("unique tool ids", len(by_id))
print()
rows = []
for i, (tid, evs) in enumerate(by_id.items(), 1):
    started = next((e for e in evs if e.get("phase") == "started"), None)
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    name = done.get("toolName") or done.get("tool_name") or (started or {}).get("toolName")
    args = done.get("arguments") if done.get("arguments") is not None else (started or {}).get("arguments")
    phase = done.get("phase")
    err = done.get("error")
    dur = done.get("durationMs") or done.get("duration_ms")
    result = done.get("result")
    rsum = ""
    if isinstance(result, dict):
        keys = list(result.keys())[:12]
        rsum = "keys=" + ",".join(str(k) for k in keys)
        for k in ("ok", "file", "action", "inbound_empty"):
            if k in result:
                rsum += f" {k}={result.get(k)}"
        if result.get("error"):
            rsum += f" err={str(result.get('error'))[:80]}"
        if "matches" in result:
            m = result["matches"]
            rsum += f" matches={len(m) if isinstance(m, list) else type(m).__name__}"
        if "control_flow" in result:
            cf = result["control_flow"]
            rsum += f" cf={len(cf) if isinstance(cf, list) else type(cf).__name__}"
    elif result is not None:
        rsum = str(result)[:160]
    arg_s = json.dumps(args, ensure_ascii=False) if args is not None else ""
    if len(arg_s) > 240:
        arg_s = arg_s[:240] + "..."
    print(f"{i:02d} step={done.get('step')} {phase} {name} {dur}ms")
    print(f"    args: {arg_s}")
    print(f"    result: {rsum} err={err}")
    rows.append(
        {
            "i": i,
            "step": done.get("step"),
            "name": name,
            "phase": phase,
            "args": args,
            "result_summary": rsum,
            "error": err,
            "durationMs": dur,
        }
    )

print("\n==== model nodes ====")
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
        )

open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8555_tools.json", "w", encoding="utf-8").write(
    json.dumps(rows, ensure_ascii=False, indent=2)
)
print("\nwrote", len(rows), "tool rows")
