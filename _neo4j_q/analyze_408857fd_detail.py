import json
import sys

sys.stdout.reconfigure(encoding="utf-8")

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_408857fd.json"
with open(path, encoding="utf-8") as f:
    data = json.load(f)

items = data.get("items") or []


def summarize_http(result):
    if result is None:
        return
    if isinstance(result, str):
        try:
            result = json.loads(result)
        except Exception:
            print("  result str", result[:400])
            return
    if not isinstance(result, dict):
        print("  result type", type(result), str(result)[:300])
        return
    status = result.get("status")
    body = result.get("body")
    print("  http status", status, "keys", list(result.keys()))
    if isinstance(body, str):
        try:
            body = json.loads(body)
        except Exception:
            print("  body str", body[:400])
            return
    if not isinstance(body, dict):
        print("  body", str(body)[:400])
        return
    hits = (body.get("hits") or {}).get("hits") or []
    total = (body.get("hits") or {}).get("total")
    print("  es total", total, "returned", len(hits), "took", body.get("took"))
    for h in hits[:8]:
        src = h.get("_source") or {}
        idx = h.get("_index")
        keys = list(src.keys())[:20]
        msg = src.get("message") or src.get("msg") or src.get("log") or ""
        level = src.get("level") or src.get("log.level") or src.get("severity")
        svc = src.get("service") or src.get("app") or src.get("kubernetes.labels.app")
        extra = {k: src.get(k) for k in ("flow_id", "flowId", "trace_id", "traceId", "error", "err", "status", "event") if src.get(k)}
        print(f"    idx={idx} level={level} svc={svc} extra={extra} msg={str(msg)[:180]!r} keys={keys}")


for mi in (1, 3):
    m = items[mi]
    print("\n" + "#" * 80)
    print("MSG", mi, m.get("createdAt"))
    tl = (m.get("metadata") or {}).get("timeline") or []
    for j, ev in enumerate(tl):
        if ev.get("kind") != "tool":
            phase = ev.get("phase")
            step = ev.get("step")
            mode = ev.get("mode")
            model = ev.get("model")
            print(f"[{j}] MODEL step={step} phase={phase} mode={mode} model={model} seq={ev.get('seq')} msgCount={ev.get('messageCount')}")
            continue
        name = ev.get("toolName")
        args = ev.get("arguments")
        print(f"[{j}] TOOL {name} step={ev.get('step')} seq={ev.get('seq')} phase={ev.get('phase')} truncated={ev.get('truncated')} decision={ev.get('decision')} allowed={ev.get('allowed')} dur={ev.get('durationMs')} error={ev.get('error')}")
        print("  args", json.dumps(args, ensure_ascii=False)[:500])
        if name == "http_request":
            summarize_http(ev.get("result"))
        elif name == "execute_read":
            print("  result", str(ev.get("result"))[:400])
            print("  error", ev.get("error"))
        elif name == "list_tables":
            print("  result", str(ev.get("result"))[:400])
        elif name == "list_tools":
            r = ev.get("result")
            s = r if isinstance(r, str) else json.dumps(r, ensure_ascii=False)
            print("  result head", s[:250])
        elif name == "load_skill":
            r = ev.get("result") or ""
            print("  skill head", str(r)[:180].replace("\n", " | "))
