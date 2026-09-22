import json

with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7.json", encoding="utf-8") as f:
    data = json.load(f)

asst = data["items"][1]
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_content.txt", "w", encoding="utf-8") as f:
    f.write(asst.get("content") or "")

user = data["items"][0]
with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_user.txt", "w", encoding="utf-8") as f:
    f.write(user.get("content") or "")

tl = asst["metadata"]["timeline"]

# dump list_tools full
for ev in tl:
    if ev.get("toolName") == "list_tools":
        path = rf"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_list_tools_step{ev.get('step')}.json"
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"args": ev.get("arguments"), "result": ev.get("result")}, f, ensure_ascii=False, indent=2)
        print("wrote", path)

# dump one successful ES body fully
for ev in tl:
    if ev.get("toolName") == "http_request" and ev.get("phase") == "completed":
        res = ev.get("result")
        body = None
        if isinstance(res, dict):
            body = res.get("body")
        elif isinstance(res, str):
            try:
                parsed = json.loads(res)
                body = parsed.get("body")
            except Exception:
                body = res
        if isinstance(body, str) and len(body) > 200:
            with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_es_body.json", "w", encoding="utf-8") as f:
                f.write(body)
            print("es body len", len(body), "step", ev.get("step"))
            try:
                b = json.loads(body)
                hits = (b.get("hits") or {}).get("hits") or []
                print("hits in stored body", len(hits))
                # extract M field samples
                msgs = []
                for h in hits[:30]:
                    src = h.get("_source") or {}
                    m = src.get("M") or src.get("message") or ""
                    msgs.append((h.get("_index"), src.get("T") or src.get("@timestamp"), str(m)[:240]))
                with open(r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_75f7_es_msgs.txt", "w", encoding="utf-8") as f:
                    for row in msgs:
                        f.write(f"{row[0]}\t{row[1]}\t{row[2]}\n")
                print("wrote msgs", len(msgs))
            except Exception as e:
                print("parse fail", e)
            break

print("done")
