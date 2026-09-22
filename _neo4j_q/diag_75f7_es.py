import json
import re

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7.json", encoding="utf-8") as f:
    data = json.load(f)

asst = data["items"][1]
content = asst.get("content") or ""
print("=== FULL ASSISTANT CONTENT ===")
print(content)
print("=== END CONTENT len=%d ===" % len(content))

tl = asst["metadata"]["timeline"]

print("\n=== HTTP_REQUEST RESULTS ===")
for ev in tl:
    if ev.get("toolName") != "http_request":
        continue
    args = ev.get("arguments") or {}
    url = args.get("url") if isinstance(args, dict) else None
    method = args.get("method") if isinstance(args, dict) else None
    res = ev.get("result")
    err = ev.get("error")
    phase = ev.get("phase")
    print(f"\n-- step={ev.get('step')} phase={phase} {method} {url}")
    print("err:", err)
    if isinstance(res, dict):
        body = res.get("body")
        print("status:", res.get("status"), res.get("status_code"))
        if isinstance(body, str):
            print("body_len:", len(body))
            # try parse es
            try:
                b = json.loads(body)
                if "hits" in b:
                    hits = b["hits"]
                    total = hits.get("total")
                    arr = hits.get("hits") or []
                    print("es_total:", total, "returned_hits:", len(arr))
                    if arr:
                        src = arr[0].get("_source") or {}
                        print("first_index:", arr[0].get("_index"))
                        print("first_source_keys:", list(src.keys())[:20])
                        msg = src.get("M") or src.get("message") or src.get("msg") or ""
                        print("first_msg:", str(msg)[:300])
                    # unique indices
                    idxs = {}
                    for h in arr:
                        idxs[h.get("_index")] = idxs.get(h.get("_index"), 0) + 1
                    print("indices:", idxs)
                elif "error" in b:
                    print("es_error:", str(b)[:400])
            except Exception as e:
                print("body_head:", body[:400], "parse_err", e)
        else:
            print("body_type", type(body), str(body)[:200] if body is not None else None)
    elif isinstance(res, str):
        print("result_str_len", len(res))
        print("result_head", res[:500])
    else:
        print("result", type(res), str(res)[:300] if res is not None else None)

print("\n=== LIST_TOOLS ===")
for ev in tl:
    if ev.get("toolName") != "list_tools":
        continue
    res = ev.get("result")
    s = res if isinstance(res, str) else json.dumps(res, ensure_ascii=False)
    print("step", ev.get("step"), "args", ev.get("arguments"))
    print("result_len", len(s))
    names = re.findall(r'"name"\s*:\s*"([^"]+)"', s)
    print("tool names:", names[:80])
    print("---")

print("\n=== APPEND_LEARNING ===")
for ev in tl:
    if ev.get("toolName") == "append_learning":
        print(json.dumps(ev.get("arguments"), ensure_ascii=False, indent=2))
        print("result", ev.get("result"))
