# -*- coding: utf-8 -*-
import json
import re
from collections import Counter

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_6257389a.json"
out_path = r"E:\workspace\github\sixath\sixath\_neo4j_q\diag_6257389a_out.txt"

with open(path, encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []
lines = []
lines.append(f"messages={len(items)}")

interrupt_hints = []

for i, m in enumerate(items):
    role = m.get("role")
    content = m.get("content") or ""
    meta = m.get("metadata") or {}
    tl = meta.get("timeline")
    created = m.get("createdAt") or m.get("created_at")
    lines.append("=" * 60)
    lines.append(
        f"msg[{i}] role={role} created={created} content_len={len(content)} "
        f"timeline={len(tl) if isinstance(tl, list) else type(tl).__name__}"
    )
    for k in sorted(meta.keys()):
        if k == "timeline":
            continue
        v = meta[k]
        if isinstance(v, (dict, list)):
            sv = json.dumps(v, ensure_ascii=False)[:300]
        else:
            sv = repr(v)[:300]
        lines.append(f"  meta.{k}={sv}")

    if content:
        lines.append("  HEAD: " + content[:300].replace("\n", "\\n"))
        lines.append("  TAIL: " + content[-400:].replace("\n", "\\n"))

    if not isinstance(tl, list):
        continue

    kinds = []
    unfinished_tools = []
    errors = []
    for j, ev in enumerate(tl):
        if not isinstance(ev, dict):
            continue
        k = ev.get("kind") or ev.get("type") or ev.get("event") or "?"
        name = ""
        tool = ev.get("tool") if isinstance(ev.get("tool"), dict) else {}
        if tool:
            name = tool.get("name") or ""
        for key in ("toolName", "tool_name", "name"):
            if not name and isinstance(ev.get(key), str):
                name = ev[key]
        status = ev.get("status") or tool.get("status") or tool.get("ok")
        err = ev.get("error") or tool.get("error")
        dur = ev.get("durationMs") or ev.get("duration_ms") or tool.get("durationMs")
        brief = {
            "i": j,
            "kind": k,
            "name": name,
            "status": status,
            "error": (str(err)[:200] if err else None),
            "dur": dur,
            "keys": list(ev.keys())[:12],
        }
        kinds.append(brief)
        if k == "tool" and status in (False, "error", "failed", "running", "pending", "canceled", "cancelled", "aborted"):
            unfinished_tools.append(brief)
        if err:
            errors.append(brief)
        # also check nested
        if tool.get("ok") is False:
            unfinished_tools.append(brief)

    lines.append(f"  kinds={Counter(x['kind'] for x in kinds)}")
    lines.append("  ALL timeline briefs:")
    for b in kinds:
        lines.append(
            f"    [{b['i']}] {b['kind']} name={b['name']!r} status={b['status']!r} "
            f"dur={b['dur']} err={b['error']!r}"
        )

    # detect incomplete turn signals
    last = kinds[-1] if kinds else None
    ends_with_tool = last and last["kind"] == "tool"
    ends_with_model_empty = False
    if last and last["kind"] == "model":
        # peek model event content
        lev = tl[last["i"]]
        mc = lev.get("content") or lev.get("text") or ""
        if isinstance(mc, str) and not mc.strip() and not content.strip():
            ends_with_model_empty = True

    # check for stop/cancel events
    s = json.dumps(tl, ensure_ascii=False)
    flags = {}
    for pat in (
        "canceled",
        "cancelled",
        "aborted",
        "interrupt",
        "interrupted",
        "max_steps",
        "MaxSteps",
        "timeout",
        "context_length",
        "finish_reason",
        "finishReason",
        "stop_reason",
        "Client disconnected",
        "context canceled",
        "context cancelled",
        "EOF",
        "broken pipe",
    ):
        n = len(re.findall(re.escape(pat), s, flags=re.I))
        if n:
            flags[pat] = n

    # look at last model event structure more carefully
    model_events = [ev for ev in tl if isinstance(ev, dict) and (ev.get("kind") == "model" or ev.get("type") == "model")]
    if model_events:
        last_model = model_events[-1]
        lines.append("  last_model_keys=" + str(list(last_model.keys())))
        for mk in ("finishReason", "finish_reason", "stopReason", "stop_reason", "status", "error", "partial", "done"):
            if mk in last_model:
                lines.append(f"  last_model.{mk}={last_model[mk]!r}")
        # dump truncated last model
        lines.append("  last_model_snip=" + json.dumps(last_model, ensure_ascii=False)[:800])

    if flags:
        lines.append(f"  FLAGS={flags}")
    if unfinished_tools:
        lines.append(f"  unfinished_tools={len(unfinished_tools)}")
    if errors:
        lines.append(f"  errors_n={len(errors)}")

    # Heuristic: short assistant reply after tools might be interrupted
    suspicious = False
    reasons = []
    if role == "assistant":
        if ends_with_tool:
            suspicious = True
            reasons.append("ends_with_tool")
        if flags:
            suspicious = True
            reasons.append("flags:" + ",".join(flags))
        if unfinished_tools:
            suspicious = True
            reasons.append("unfinished_tools")
        # very short final answer relative to tool activity
        tool_n = sum(1 for x in kinds if x["kind"] == "tool")
        if tool_n >= 3 and len(content) < 500:
            suspicious = True
            reasons.append(f"short_after_{tool_n}_tools")
        # content looks truncated mid-sentence
        if content and not content.rstrip().endswith(("。", "！", "？", ".", "!", "?", "```", ")", "）", ">", "|")):
            if len(content) > 50:
                suspicious = True
                reasons.append("tail_not_sentence_end")
        if "Traceback" in content or "context canceled" in content.lower() or "cancelled" in content.lower():
            suspicious = True
            reasons.append("error_in_content")

    if suspicious:
        interrupt_hints.append((i, reasons, len(content), len(kinds)))
        lines.append(f"  **SUSPICIOUS** reasons={reasons}")

lines.append("=" * 60)
lines.append("SUSPICIOUS TURNS SUMMARY:")
for row in interrupt_hints:
    lines.append(f"  msg{row[0]}: reasons={row[1]} content_len={row[2]} events={row[3]}")

# Also dump raw meta of suspicious / short assistants
lines.append("=" * 60)
lines.append("DETAILED SUSPICIOUS / SHORT ASSISTANTS:")
for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    content = m.get("content") or ""
    tl = (m.get("metadata") or {}).get("timeline")
    ntl = len(tl) if isinstance(tl, list) else 0
    if len(content) < 800 or i in [h[0] for h in interrupt_hints]:
        lines.append(f"\n--- detail msg[{i}] len={len(content)} ntl={ntl} ---")
        lines.append(content)
        lines.append("--- meta without timeline ---")
        meta = dict(m.get("metadata") or {})
        meta.pop("timeline", None)
        lines.append(json.dumps(meta, ensure_ascii=False, indent=2)[:2000])

text = "\n".join(lines)
with open(out_path, "w", encoding="utf-8") as f:
    f.write(text)
print("wrote", out_path, "chars", len(text), "suspicious", len(interrupt_hints))
