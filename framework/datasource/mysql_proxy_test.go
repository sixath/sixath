package datasource

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestNewMySQLDataSource_UsesDialContext(t *testing.T) {
	dialed := false
	stubErr := errors.New("stub dial")
	cfg := Config{
		ID:     "proxy-mysql-1",
		Host:   "127.0.0.1",
		Port:   1,
		User:   "u",
		DBName: "db",
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = true
			return nil, stubErr
		},
	}
	ds, err := NewMySQLDataSource(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ds.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = ds.Ping(ctx)
	if !dialed {
		t.Fatal("DialContext was not invoked")
	}
	if err == nil {
		t.Fatal("expected ping error from stub DialContext")
	}
}

func TestMySQLProxyNetName_IncludesProxyNetKey(t *testing.T) {
	a := mysqlProxyNetName(Config{ID: "orders", ProxyNetKey: "lab"})
	b := mysqlProxyNetName(Config{ID: "orders", ProxyNetKey: "office"})
	if a == b {
		t.Fatalf("same ds id with different ProxyNetKey must not collide: %s", a)
	}
	if a != "orders-lab" || b != "orders-office" {
		t.Fatalf("names=%q %q", a, b)
	}
	legacy := mysqlProxyNetName(Config{ID: "orders"})
	if legacy != "sixath-proxy-orders" {
		t.Fatalf("legacy name=%q", legacy)
	}
}
