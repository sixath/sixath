package handbook

import (
	"fmt"
	"strings"
	"testing"
)

func hitsString(hs []RegisterHit) string {
	var out []string
	for _, h := range hs {
		out = append(out, fmt.Sprintf("%s/%s/%s@%d", h.Kind, h.Name, h.Access, h.Line))
	}
	return strings.Join(out, " ")
}

func TestScanRegisters_GoSource(t *testing.T) {
	src := strings.Join([]string{
		`q := "SELECT o.id FROM orders o JOIN users u ON u.id = o.uid"`, // 1
		`db.Exec("INSERT INTO order_logs (id) VALUES (?)")`,             // 2
		`db.Exec("UPDATE orders SET status = ?")`,                       // 3
		`db.Exec("DELETE FROM carts WHERE id = ?")`,                     // 4
		`db.Table("coupons").Where("id = ?", id)`,                       // 5
		`r.GET("/api/v1/orders/:id", h.Get)`,                            // 6
		`mux.HandleFunc("/healthz", health)`,                            // 7
		`const OrderTopic = "order-events"`,                             // 8
		`rdb.Set(ctx, fmt.Sprintf("order:detail:%d", id), v, ttl)`,      // 9
		`val, _ := rdb.Get(ctx, "order:detail:"+id).Result()`,           // 10
		`log.Printf("read from file")`,                                  // 11
		`url := "http://example.com:8080/x"`,                            // 12
		`layout := "15:04:05"`,                                          // 13
	}, "\n")
	got := hitsString(dedupeRegisters(scanRegisters("svc/order.go", "go", []byte(src))))
	want := "table/orders/read@1 table/users/read@1 table/order_logs/write@2 table/orders/write@3 table/carts/write@4 table/coupons/ref@5 " +
		"route//api/v1/orders/:id/serve@6 route//healthz/serve@7 topic/order-events/ref@8 " +
		"cache_key/order:detail:/write@9 cache_key/order:detail:/read@10"
	if got != want {
		t.Fatalf("hits:\n got %s\nwant %s", got, want)
	}
}

func TestScanRegisters_SQLProtoYAML(t *testing.T) {
	sql := "select * from payments;\ninsert into refunds values (1);\n"
	if got := hitsString(scanRegisters("db/q.sql", "sql", []byte(sql))); got != "table/payments/read@1 table/refunds/write@2" {
		t.Fatalf("sql: %s", got)
	}
	proto := "option (google.api.http) = { get: \"/v1/orders/{id}\" };\n"
	if got := hitsString(scanRegisters("api/o.proto", "proto", []byte(proto))); got != "route//v1/orders/{id}/serve@1" {
		t.Fatalf("proto: %s", got)
	}
	yaml := "kafka:\n  order_topic: order-events\n  brokers: a:9092\n"
	if got := hitsString(scanRegisters("conf/app.yaml", "yaml", []byte(yaml))); got != "topic/order-events/ref@2" {
		t.Fatalf("yaml: %s", got)
	}
	if got := scanRegisters("README.md", "markdown", []byte("SELECT * FROM orders")); len(got) != 0 {
		t.Fatalf("markdown must not be scanned: %v", got)
	}
}

func TestSortRegisters_WritesFirst(t *testing.T) {
	hs := []RegisterHit{
		{Kind: RegTable, Name: "orders", Access: AccessRead, Path: "a.go", Line: 1},
		{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "z.go", Line: 9},
		{Kind: RegRoute, Name: "/a", Access: AccessServe, Path: "r.go", Line: 1},
	}
	sortRegisters(hs)
	if got := hitsString(hs); got != "route//a/serve@1 table/orders/write@9 table/orders/read@1" {
		t.Fatalf("sorted: %s", got)
	}
}
