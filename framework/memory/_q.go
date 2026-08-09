package main
import (
  "context"
  "fmt"
  "os"
  "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)
func main() {
  pass := os.Getenv("NEO4J_PASSWORD")
  d, err := neo4j.NewDriverWithContext("bolt://127.0.0.1:7687", neo4j.BasicAuth("neo4j", pass, ""))
  if err != nil { panic(err) }
  defer d.Close(context.Background())
  s := d.NewSession(context.Background(), neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
  defer s.Close(context.Background())
  sid := "b73cf880-f42f-497a-8672-3fd39414fb2a"
  res, err := s.Run(context.Background(), `MATCH (a:MemoryEntity)-[r:REL]->(b:MemoryEntity) WHERE a.scope_id=$sid RETURN count(r) AS c`, map[string]any{"sid": sid})
  if err != nil { panic(err) }
  rec, err := res.Single(context.Background())
  if err != nil { panic(err) }
  fmt.Println("session_rels", rec.Values[0])
  res2, _ := s.Run(context.Background(), `MATCH (a:MemoryEntity)-[r:REL]->(b:MemoryEntity) WHERE a.scope_id=$sid RETURN a.name, r.predicate, b.name ORDER BY a.name LIMIT 30`, map[string]any{"sid": sid})
  for res2.Next(context.Background()) {
    v := res2.Record().Values
    fmt.Printf("%v -%v-> %v\n", v[0], v[1], v[2])
  }
  res3, _ := s.Run(context.Background(), `MATCH (n:MemoryEntity) RETURN count(n); MATCH ()-[r:REL]->() RETURN count(r)`, nil)
  // two queries separately
  r3, _ := s.Run(context.Background(), `MATCH (n:MemoryEntity) RETURN count(n) AS n`, nil)
  rec3,_ := r3.Single(context.Background())
  r4, _ := s.Run(context.Background(), `MATCH ()-[r:REL]->() RETURN count(r) AS n`, nil)
  rec4,_ := r4.Single(context.Background())
  fmt.Println("total_entities", rec3.Values[0], "total_rels", rec4.Values[0])
}
