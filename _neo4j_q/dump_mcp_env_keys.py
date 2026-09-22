import json
import sys

import pymysql

sys.stdout.reconfigure(encoding="utf-8")

conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()
cur.execute(
    "SELECT id, name, transport, command, args_json, env_json, endpoint FROM mcp_servers"
)
for row in cur.fetchall():
    sid, name, transport, command, args, env, endpoint = row
    keys = []
    url_val = ""
    if env:
        if isinstance(env, (bytes, bytearray)):
            env = env.decode("utf-8")
        data = json.loads(env) if isinstance(env, str) else env
        if isinstance(data, dict):
            keys = list(data.keys())
            for k, v in data.items():
                if "URL" in k.upper() or "HOST" in k.upper():
                    url_val = str(v)
    print(f"id={sid!r} name={name!r} cmd={command!r}")
    print(f"  args={args}")
    print(f"  env_keys={keys}")
    if url_val:
        print(f"  url={url_val}")
    print()
