package datasource

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestNewMongoDataSource_UsesDialContext(t *testing.T) {
	dialed := false
	stubErr := errors.New("stub dial")
	cfg := Config{
		ID:     "proxy-mongo-1",
		Host:   "127.0.0.1",
		Port:   1,
		User:   "u",
		DBName: "db",
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = true
			return nil, stubErr
		},
	}
	ds, err := NewMongoDataSource(cfg)
	if ds != nil {
		t.Cleanup(func() { _ = ds.Close() })
	}
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = ds.Ping(ctx)
	}
	if !dialed {
		t.Fatal("DialContext was not invoked")
	}
	if err == nil {
		t.Fatal("expected connect/ping error from stub DialContext")
	}
}
