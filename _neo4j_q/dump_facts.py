import json
import pymysql

try:
    from neo4j import GraphDatabase
except ImportError:
    GraphDatabase = None

sid = "64387b26-dd00-4b80-831b-98b2d999ac9c"

conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()

print("=== messages ===")
cur.execute(
    "SELECT role, content, created_at FROM chat_messages WHERE session_id=%s ORDER BY created_at",
    (sid,),
)
for role, content, ts in cur.fetchall():
    print(f"\n[{role}] {ts}")
    print(content[:1200])

print("\n=== memory_units full ===")
cur.execute(
    """
    SELECT id, scope_type, scope_id, kind, status, content, metadata, created_at
    FROM memory_units
    WHERE source_session_id=%s OR (scope_type='session' AND scope_id=%s)
    ORDER BY created_at
    """,
    (sid, sid),
)
for row in cur.fetchall():
    print("\n--- unit", row[0], row[3], row[7])
    print("content:", row[5])
    meta = row[6]
    if isinstance(meta, (bytes, bytearray)):
        meta = meta.decode("utf-8")
    if isinstance(meta, str):
        try:
            meta = json.loads(meta)
        except Exception:
            pass
    print("meta:", json.dumps(meta, ensure_ascii=False, indent=2) if not isinstance(meta, str) else meta)

if GraphDatabase is None:
    print("\nneo4j driver missing")
else:
    drv = GraphDatabase.driver("bolt://localhost:7687", auth=("neo4j", "jw123456"))
    with drv.session() as sess:
        print("\n=== neo4j entities ===")
        recs = sess.run(
            """
            MATCH (n:MemoryEntity)
            WHERE n.scope_id = $sid
            RETURN n.name AS name, n.entity_type AS type, n.confidence AS conf,
                   n.scope_type AS scope, n.source_memory_id AS src
            ORDER BY n.name
            """,
            sid=sid,
        )
        for r in recs:
            print(dict(r))
        print("\n=== neo4j relations ===")
        recs = sess.run(
            """
            MATCH (a:MemoryEntity)-[rel:REL]->(b:MemoryEntity)
            WHERE rel.scope_id = $sid OR a.scope_id = $sid
            RETURN a.name AS subj, rel.predicate AS pred, b.name AS obj,
                   rel.confidence AS conf, rel.source_memory_id AS src
            ORDER BY rel.predicate, a.name
            """,
            sid=sid,
        )
        for r in recs:
            print(dict(r))
    drv.close()
