import json, re, urllib.request
from collections import Counter

url = "http://127.0.0.1:8000/api/v1/sessions/bf29ace1-c456-44a5-be11-ede6e3217454/messages"
req = urllib.request.Request(url, headers={"Authorization": "Bearer dev-bootstrap-token"})
data = json.load(urllib.request.urlopen(req, timeout=30))
s = json.dumps(data, ensure_ascii=False)

c = Counter(re.findall(r'"name"\s*:\s*"([a-zA-Z0-9_\-]+)"', s))
interesting = [
    (k, v)
    for k, v in c.most_common(120)
    if any(
        x in k.lower()
        for x in [
            "ssh",
            "scp",
            "rca",
            "grep",
            "glob",
            "read",
            "memory",
            "terminal",
            "code",
            "exec",
            "search",
            "file",
            "list",
            "write",
        ]
    )
]
print("interesting names:", interesting)
for pat in [
    "migu-rca",
    "mg-rca",
    "rca_code",
    "rca_grep",
    "rca_glob",
    "rca_read",
    "code_grep",
    "code_glob",
    "code_read",
]:
    print(pat, len(re.findall(re.escape(pat), s, flags=re.I)))
