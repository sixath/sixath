# -*- coding: utf-8 -*-
"""Reproduce skill RouteBest scoring for the live query."""
import re
import unicodedata

q = "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功"
q = q.lower().strip()

def tokenize(s):
    out = set()
    buf = []
    def flush():
        if len(buf) >= 3:
            out.add("".join(buf))
        buf.clear()
    for ch in s:
        if ch.isalpha() or ch.isdigit():
            buf.append(ch)
        else:
            flush()
    flush()
    return out

q_tokens = tokenize(q)
print("q tokens:", sorted(q_tokens))

skills = [
    {
        "name": "rca-sync-archive-migrate",
        "description": "实时存档迁移（SyncDispatch, Mode=1）链路排障。用于排查 union_resource 触发的实时存档跨区域迁移问题，覆盖 union_resource → union-archiver-dispatch(uad) → union-archiver-manager(uam) → archiver-manager(am) → data-channel → Kafka 回调 → 源区域清理的完整链路，基于 ES 日志（mg-rca-es）逐跳定位。",
        "tags": ["rca", "archive-migrate", "sync-dispatch", "es", "union-resource"],
    },
    {
        "name": "migu-cloud-game-vm-allocate",
        "description": "use when troubleshooting cloud game scheduling issues in the migu monorepo (vm allocation, streaming, release, archive migration). covers the complete call chain across access-service, union-access, union_resource, vm-manager, usscheduler, data_channel, gsm, and archive migration services. includes v1 and v2 allocation paths, common error codes, and troubleshooting entry points.",
        "tags": [],
    },
]

def score(m):
    score = 0
    details = []
    name = m["name"].lower().strip()
    name_spaced = name.replace("-", " ")
    if name in q or name_spaced in q:
        score += 12
        details.append("name_in_q +12")
    for part in name.split("-"):
        part = part.strip()
        if len(part) < 3:
            continue
        if part in q_tokens:
            score += 4
            details.append(f"name_part {part} +4")
    desc = m["description"].lower()
    for tok in q_tokens:
        if len(tok) < 3:
            continue
        if tok in desc:
            score += 2
            details.append(f"desc_tok {tok} +2")
    for tag in m["tags"]:
        tag = tag.lower().strip()
        if not tag:
            continue
        in_q = tag in q
        in_tok = False
        for part in tag.replace("-", " ").split():
            if len(part) >= 3 and part in q_tokens:
                in_tok = True
        if in_q or in_tok:
            score += 5
            how = "substr" if in_q else "token"
            details.append(f"tag {tag} +5 ({how})")
            if in_q and not in_tok:
                # show why substring matched
                idx = q.find(tag)
                details.append(f"  q[{idx}:{idx+len(tag)+4}]={q[max(0,idx-6):idx+len(tag)+6]!r}")
    return score, details

for m in skills:
    sc, det = score(m)
    print(f"\n{m['name']} score={sc}")
    for d in det:
        print(" ", d)
