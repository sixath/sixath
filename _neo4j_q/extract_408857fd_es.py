import json
import re
import sys

sys.stdout.reconfigure(encoding="utf-8")

path = r"E:\workspace\github\sixath\sixath\_neo4j_q\sess_408857fd.json"
with open(path, encoding="utf-8") as f:
    data = json.load(f)

# Extract ES-like messages from ALL tool results of last assistant turn
m = data["items"][3]
tl = m["metadata"]["timeline"]


def walk_text(obj, acc):
    if isinstance(obj, str):
        acc.append(obj)
    elif isinstance(obj, dict):
        for v in obj.values():
            walk_text(v, acc)
    elif isinstance(obj, list):
        for v in obj:
            walk_text(v, acc)


texts = []
for ev in tl:
    if ev.get("kind") != "tool":
        continue
    if ev.get("toolName") != "http_request":
        continue
    r = ev.get("result")
    if r is None:
        continue
    if isinstance(r, str):
        texts.append(r)
    else:
        walk_text(r, texts)

blob = "\n".join(texts)
print("blob len", len(blob))

# Try parse JSON body of last successful ES search with vm_manager
for ev in tl:
    if ev.get("toolName") != "http_request":
        continue
    r = ev.get("result")
    s = r if isinstance(r, str) else json.dumps(r, ensure_ascii=False)
    print("\n--- tool seq", ev.get("seq"), "truncated", ev.get("truncated"), "len", len(s), "url", str((ev.get("arguments") or {}).get("url", ""))[-80:])
    print("head:", s[:300])
    print("tail:", s[-400:])

# Pull interesting fragments
for pat in [
    r"error[^\n]{0,120}",
    r"fail[^\n]{0,120}",
    r"失败[^\n]{0,80}",
    r"timeout[^\n]{0,80}",
    r"refuse[^\n]{0,80}",
    r"reject[^\n]{0,80}",
    r"recycle[^\n]{0,80}",
    r"startGame[^\n]{0,80}",
    r"flow_id[^\n]{0,80}",
    r"\"M\":\"[^\"]{0,200}",
]:
    ms = re.findall(pat, blob, flags=re.I)
    if ms:
        print("\nPAT", pat, "n=", len(ms))
        for x in ms[:8]:
            print(" ", x[:200])
