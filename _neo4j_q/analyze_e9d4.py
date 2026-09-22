import json
import sys
from collections import Counter

sys.stdout.reconfigure(encoding="utf-8")

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_e9d4.json", encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []

# dump each assistant turn in detail
for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    meta = m.get("metadata") or {}
    tl = meta.get("timeline") if isinstance(meta, dict) else []
    print("=" * 80)
    print(f"ASST [{i}] {m.get('createdAt')} clen={len(m.get('content') or '')} events={len(tl) if isinstance(tl, list) else 0}")
    print("CONTENT:")
    print((m.get("content") or "")[:1500])
    print("---")
    if not isinstance(meta, dict):
        continue
    # interesting meta keys
    skip = {"timeline"}
    for k, v in meta.items():
        if k in skip:
            continue
        s = json.dumps(v, ensure_ascii=False) if not isinstance(v, str) else v
        if len(s) > 600:
            s = s[:600] + "..."
        print(f"META {k}: {s}")

    if not isinstance(tl, list):
        continue
    print("\nTIMELINE:")
    for ev in tl:
        if not isinstance(ev, dict):
            continue
        kind = ev.get("kind")
        phase = ev.get("phase")
        step = ev.get("step")
        name = ev.get("toolName") or ev.get("model") or ""
        err = ev.get("error") or ""
        args = ev.get("arguments")
        extra = []
        for k in (
            "dropped",
            "droppedProposals",
            "goalDriftNudges",
            "retry",
            "reason",
            "policy",
            "gate",
            "nudge",
            "message",
            "blocked",
            "family",
            "skill",
        ):
            if ev.get(k) not in (None, "", [], {}):
                extra.append(f"{k}={json.dumps(ev.get(k), ensure_ascii=False)[:220]}")
        arg_s = ""
        if args is not None:
            arg_s = json.dumps(args, ensure_ascii=False)
            if len(arg_s) > 220:
                arg_s = arg_s[:220] + "..."
        line = f"  step={step} {kind}/{phase} {name}"
        if err:
            line += f" ERR={str(err)[:180]}"
        if arg_s:
            line += f" args={arg_s}"
        if extra:
            line += " | " + " | ".join(extra)
        print(line)
        # print result snippet for key tools
        result = ev.get("result")
        if kind == "tool" and phase in ("completed", "failed", None) and result is not None:
            rs = result if isinstance(result, str) else json.dumps(result, ensure_ascii=False)
            if name in ("es_log_query", "rca_read", "rca_grep", "rca_glob", "load_skill", "skills_list", "read_skill_file"):
                print("    result:", rs[:400].replace("\n", " | "))
