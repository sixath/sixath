# -*- coding: utf-8 -*-
import json

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_bf26.json", encoding="utf-8") as f:
    data = json.load(f)

asst = data["items"][3]
tl = asst["metadata"]["timeline"]

out_lines = []
out_lines.append("=== ASSISTANT CONTENT ===")
out_lines.append(asst.get("content") or "")
out_lines.append("\n=== MODEL STEPS ===")
for ev in tl:
    if ev.get("kind") != "model":
        continue
    step = ev.get("step")
    phase = ev.get("phase")
    content = ev.get("content") or ev.get("text") or ""
    tool_calls = ev.get("toolCalls") or ev.get("tool_calls") or ev.get("calls")
    extra = {k: ev.get(k) for k in ev.keys() if k not in ("kind", "step", "phase", "content", "text", "toolCalls", "tool_calls", "calls", "arguments", "result")}
    out_lines.append(f"\n-- model step={step} phase={phase} keys={list(ev.keys())}")
    if content:
        out_lines.append("CONTENT:\n" + str(content)[:1500])
    if tool_calls:
        out_lines.append("TOOL_CALLS:\n" + json.dumps(tool_calls, ensure_ascii=False)[:1500])
    # dump remaining interesting fields
    for k, v in extra.items():
        if v in (None, "", [], {}):
            continue
        s = v if isinstance(v, str) else json.dumps(v, ensure_ascii=False)
        out_lines.append(f"{k}: {s[:400]}")

out_lines.append("\n=== TOOL ERRORS / ASK_USER / SKILL ===")
for ev in tl:
    if ev.get("kind") != "tool":
        continue
    name = ev.get("toolName")
    if name in ("ask_user", "skill_view", "execute_skill_script", "memory_recall", "rca_grep") or ev.get("error") or ev.get("phase") == "failed":
        out_lines.append(f"\n-- {name} step={ev.get('step')} phase={ev.get('phase')}")
        out_lines.append("args: " + json.dumps(ev.get("arguments"), ensure_ascii=False)[:800])
        res = ev.get("result")
        if isinstance(res, (dict, list)):
            out_lines.append("result: " + json.dumps(res, ensure_ascii=False)[:1200])
        else:
            out_lines.append("result: " + str(res)[:1200])
        if ev.get("error"):
            out_lines.append("error: " + str(ev.get("error"))[:500])

text = "\n".join(out_lines)
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_bf26_model.txt", "w", encoding="utf-8") as f:
    f.write(text)
print("wrote model dump, chars", len(text))
