# -*- coding: utf-8 -*-
import json

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_6257389a.json"
with open(path, encoding="utf-8") as f:
    items = json.load(f)["items"]

# Measure tool result sizes and error rates for confluence across all turns
out = []
for i, m in enumerate(items):
    if m.get("role") != "assistant":
        continue
    tl = (m.get("metadata") or {}).get("timeline") or []
    conf_errs = 0
    conf_ok = 0
    sizes = []
    for ev in tl:
        if not isinstance(ev, dict) or ev.get("kind") != "tool":
            continue
        name = ev.get("toolName") or ""
        if "confluence" not in name:
            continue
        result = ev.get("result") or ""
        if isinstance(result, dict):
            result = json.dumps(result, ensure_ascii=False)
        n = len(result) if isinstance(result, str) else 0
        sizes.append((name, n, ev.get("arguments"), "isError" in result or "MCP error" in result or '"isError":true' in result.replace(" ", "")))
        if "MCP error" in result or "Required at contentId" in result:
            conf_errs += 1
        elif n > 100:
            conf_ok += 1
    if sizes:
        out.append(f"msg[{i}] content_len={len(m.get('content') or '')} conf_calls={len(sizes)} errs={conf_errs} ok={conf_ok}")
        for name, n, args, iserr in sizes:
            out.append(f"  {name} result_len={n} err={iserr} args={json.dumps(args, ensure_ascii=False)[:200]}")

# Also dump full turn_trace payloads for empty/short turns
import pymysql
conn = pymysql.connect(host="172.26.50.161", port=23306, user="viu_root", password="myviu_4359", database="sath", charset="utf8mb4")
cur = conn.cursor()
cur.execute(
    "SELECT turn_seq, request_id, created_at, CHAR_LENGTH(payload_json), payload_json FROM turn_traces WHERE session_id=%s ORDER BY turn_seq",
    ("6257389a-4712-4e17-9977-dd8f161592a8",),
)
out.append("\n=== turn_trace summaries ===")
for turn_seq, rid, created, plen, payload in cur.fetchall():
    obj = json.loads(payload)
    calls = obj.get("calls") or []
    out.append(f"turn_seq={turn_seq} rid={rid} at={created} payload_len={plen} model_calls={obj.get('model_calls')} n_calls={len(calls)}")
    for c in calls:
        rp = c.get("result_preview") or ""
        err = "MCP error" in rp or "Required at contentId" in rp
        out.append(f"  step={c.get('step')} tool={c.get('tool_name')} args={json.dumps(c.get('arguments'), ensure_ascii=False)[:180]} result_preview_len={len(rp)} errish={err}")
        if err:
            # show error snippet
            idx = rp.find("MCP error")
            out.append(f"    ERR: {rp[idx:idx+180] if idx>=0 else rp[:180]}")
        elif len(rp) > 5000:
            out.append(f"    LARGE_OK_RESULT len={len(rp)}")

# Check agent max steps
cur.execute("SELECT id, name, config_json FROM agents WHERE id=%s", ("a3af7bc6-6888-4dde-b782-ef2bfcb04df1",))
row = cur.fetchone()
if row:
    out.append(f"\n=== agent === id={row[0]} name={row[1]}")
    cfg = row[2]
    if isinstance(cfg, str):
        try:
            cfg = json.loads(cfg)
        except Exception:
            pass
    out.append(json.dumps(cfg, ensure_ascii=False, indent=2)[:3000] if not isinstance(cfg, str) else cfg[:3000])

conn.close()
text = "\n".join(out)
open(r"E:\workspace\github\sixath\sixath\_neo4j_q\diag_6257389a_root.txt", "w", encoding="utf-8").write(text)
print(text[:4000])
print("\n... wrote full to diag_6257389a_root.txt")
