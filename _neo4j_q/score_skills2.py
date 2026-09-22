# -*- coding: utf-8 -*-
q = "需要看看access-service有没有收到游戏启动成功事件的时间和vm-manager有没有startGame成功".lower().strip()

def tokenize(s):
    out = set()
    buf = []
    def flush():
        tok = "".join(buf)
        if len(tok) >= 3:
            out.add(tok)
        buf.clear()
    for ch in s:
        if ch.isalpha() or ch.isdigit():  # Python isalpha includes CJK, same as unicode.IsLetter
            buf.append(ch)
        else:
            flush()
    flush()
    return out

q_tokens = tokenize(q)
print("tokens:")
for t in sorted(q_tokens, key=len):
    print(" ", repr(t))

print("\n'ss' in q?", "es" in q)
print("index of es:", q.find("es"), "context:", repr(q[max(0,q.find("es")-8):q.find("es")+8]))

skills = [
    {
        "name": "rca-sync-archive-migrate",
        "description": "实时存档迁移（SyncDispatch, Mode=1）链路排障。用于排查 union_resource 触发的实时存档跨区域迁移问题，覆盖 union_resource → union-archiver-dispatch(uad) → union-archiver-manager(uam) → archiver-manager(am) → data-channel → Kafka 回调 → 源区域清理的完整链路，基于 ES 日志（mg-rca-es）逐跳定位。".lower(),
        "tags": ["rca", "archive-migrate", "sync-dispatch", "es", "union-resource"],
    },
    {
        "name": "migu-cloud-game-vm-allocate",
        "description": ("Use when troubleshooting cloud game scheduling issues in the migu monorepo "
                        "(VM allocation, streaming, release, archive migration). Covers the complete "
                        "call chain across access-service, union-access, union_resource, vm-manager, "
                        "usscheduler, data_channel, GSM, and archive migration services. Includes "
                        "v1 and v2 allocation paths, common error codes, and troubleshooting entry points.").lower(),
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
            details.append("name_part %s +4" % part)
    desc = m["description"]
    for tok in q_tokens:
        if len(tok) < 3:
            continue
        if tok in desc:
            score += 2
            details.append("desc_tok %s +2" % tok)
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
            details.append("tag %r +5 in_q=%s in_tok=%s" % (tag, in_q, in_tok))
    return score, details

for m in skills:
    sc, det = score(m)
    print("\n%s score=%d (threshold 5)" % (m["name"], sc))
    for d in det:
        print(" ", d)
