import json, re, sys, urllib.request
from collections import Counter

url = "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
with urllib.request.urlopen(req, timeout=30) as resp:
    data = json.load(resp)

items = data.get("items") or []
print("messages:", len(items))
for i in items:
    if i.get("role") == "user":
        print("USER:", (i.get("content") or "")[:400])
        break

s = json.dumps(data, ensure_ascii=False)
tools = Counter(
    re.findall(
        r'"(?:name|tool|toolName|func_path|funcPath)"\s*:\s*"(ssh_exec|scp|rca_code|es_log_query|memory_recall|memory_search|read_file|workspace_read|list_files|grep_files|run_command|terminal_local|terminal)"',
        s,
    )
)
print("tool mentions:", dict(tools))
print("pathguard:", len(re.findall("pathguard", s)))
print("D:\\\\workspace\\\\migu:", len(re.findall(r"D:\\\\workspace\\\\migu", s)))
print("E:\\\\sixath\\\\workspace\\\\migu:", len(re.findall(r"E:\\\\sixath\\\\workspace\\\\migu", s)))

# collect ssh_exec command args
cmds = re.findall(r'"command"\s*:\s*"([^"]{0,200})"', s)
print("sample commands:")
for c in cmds[:12]:
    print(" -", c[:180].encode("unicode_escape").decode())
