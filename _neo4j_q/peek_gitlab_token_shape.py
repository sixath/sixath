import json
import sys
import pymysql

sys.stdout.reconfigure(encoding="utf-8")
conn = pymysql.connect(
    host="172.26.50.161", port=23306, user="viu_root",
    password="myviu_4359", database="sath", charset="utf8mb4",
)
cur = conn.cursor()
cur.execute("SELECT env_json FROM mcp_servers WHERE id=%s", ("gitlab",))
row = cur.fetchone()
env = row[0]
if isinstance(env, (bytes, bytearray)):
    env = env.decode("utf-8")
data = json.loads(env) if isinstance(env, str) else env
tok = str(data.get("GITLAB_PERSONAL_ACCESS_TOKEN") or "")
url = str(data.get("GITLAB_API_URL") or "")
print("url", url)
print("keys", sorted(data.keys()))
print("token_len", len(tok))
print("token_prefix", tok[:6] if tok else "")
print("has_space", any(c.isspace() for c in tok))
print("looks_masked", tok in ("***", "……", "...") or set(tok) <= set(".*•"))
