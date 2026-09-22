import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
data = json.load(open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_8555_full.json", encoding="utf-8"))
asst = [it for it in data["items"] if it.get("role") == "assistant"][-1]
tl = asst["metadata"]["timeline"]

by_id = {}
for ev in tl:
    if not isinstance(ev, dict) or ev.get("kind") != "tool":
        continue
    by_id.setdefault(ev.get("id"), []).append(ev)


def compact_result(result, name):
    if result is None:
        return None
    if isinstance(result, str):
        try:
            result = json.loads(result)
        except Exception:
            return result[:400]
    if not isinstance(result, dict):
        return result
    out = {}
    for k in ("ok", "error", "error_code", "file", "repo", "start_line", "end_line", "count"):
        if k in result:
            out[k] = result[k]
    if "matches" in result and isinstance(result["matches"], list):
        ms = []
        for m in result["matches"][:15]:
            if isinstance(m, dict):
                ms.append(
                    {
                        "path": m.get("path") or m.get("file"),
                        "line": m.get("line") or m.get("start_line"),
                        "text": str(m.get("text") or m.get("content") or m.get("snippet") or "")[:120],
                    }
                )
            else:
                ms.append(str(m)[:120])
        out["matches_n"] = len(result["matches"])
        out["matches_head"] = ms
        refs = result.get("evidence_refs")
        if isinstance(refs, list):
            out["refs_n"] = len(refs)
            paths = []
            for r in refs:
                if isinstance(r, dict):
                    paths.append(r.get("path"))
            from collections import Counter

            c = Counter(p.split("/")[0] if p else "?" for p in paths)
            out["refs_top_dirs"] = c.most_common(12)
    if "content" in result and isinstance(result["content"], str):
        out["content_len"] = len(result["content"])
        out["content_head"] = result["content"][:250]
    if "Rows" in result:
        out["rows_n"] = len(result["Rows"]) if isinstance(result["Rows"], list) else result["Rows"]
        out["columns"] = result.get("Columns")
        if isinstance(result.get("Rows"), list) and result["Rows"]:
            out["row0_keys"] = list(result["Rows"][0].keys()) if isinstance(result["Rows"][0], dict) else type(result["Rows"][0]).__name__
    if "hits" in result and isinstance(result["hits"], list):
        out["hits_n"] = len(result["hits"])
        for h in result["hits"][:3]:
            if isinstance(h, dict):
                c = ""
                if isinstance(h.get("anchor"), dict):
                    c = str(h["anchor"].get("content") or "")[:160]
                else:
                    c = str(h.get("content") or h.get("text") or "")[:160]
                out.setdefault("hit_heads", []).append(c)
    if "control_flow" in result:
        cf = result["control_flow"]
        out["cf_type"] = type(cf).__name__
        if isinstance(cf, list):
            out["cf_n"] = len(cf)
            out["cf_head"] = str(cf[:2])[:300]
        elif isinstance(cf, dict):
            out["cf_keys"] = list(cf.keys())[:10]
    if "call_graph" in result and isinstance(result["call_graph"], dict):
        nodes = result["call_graph"].get("nodes") or []
        names = []
        for n in nodes[:20]:
            if isinstance(n, dict):
                names.append(f"{n.get('name')} resolved={n.get('resolved')}")
        out["cg_nodes"] = names
    return out


print("===== TOOL DETAIL =====")
for i, (tid, evs) in enumerate(by_id.items(), 1):
    done = next((e for e in evs if e.get("phase") in ("completed", "failed")), evs[-1])
    name = done.get("toolName")
    args = done.get("arguments")
    result = done.get("result")
    print(f"\n===== {i:02d} step={done.get('step')} {name} =====")
    print("args:", json.dumps(args, ensure_ascii=False)[:400])
    compact = compact_result(result, name)
    print("result:", json.dumps(compact, ensure_ascii=False, indent=2)[:2500])

# step gaps: model steps with no tools
tool_steps = set()
for evs in by_id.values():
    for e in evs:
        tool_steps.add(e.get("step"))
model_steps = sorted({e.get("step") for e in tl if e.get("kind") == "model"})
print("\n===== STEPS WITHOUT TOOLS =====")
print("model steps", model_steps)
print("tool steps", sorted(tool_steps))
print("model-only", [s for s in model_steps if s not in tool_steps])
