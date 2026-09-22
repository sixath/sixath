package datasource

import (
	"strings"
	"testing"
)

func TestMongoDBNameFromDSN(t *testing.T) {
	cases := []struct {
		dsn  string
		want string
	}{
		{"", ""},
		{"mongodb://u:p@host:27017/appdb?authSource=admin", "appdb"},
		{"mongodb://host:27017/zijian", "zijian"},
		{"mongodb://host:27017/", ""},
		{"mongodb://host:27017", ""},
	}
	for _, tc := range cases {
		if got := mongoDBNameFromDSN(tc.dsn); got != tc.want {
			t.Fatalf("dsn=%q: got %q want %q", tc.dsn, got, tc.want)
		}
	}
}

func TestNewMongoDataSource_MissingDBName(t *testing.T) {
	_, err := NewMongoDataSource(Config{ID: "zijian_mongodb_sit", Host: "127.0.0.1", Port: 27017})
	if err == nil || !strings.Contains(err.Error(), "missing dbname") {
		t.Fatalf("expected missing dbname, got %v", err)
	}
}

func TestNewMongoDataSource_DBNameFromDSNPath(t *testing.T) {
	// Connect will fail (no server); assert we get past dbname validation.
	_, err := NewMongoDataSource(Config{
		ID:  "zijian_mongodb_sit",
		DSN: "mongodb://127.0.0.1:1/appdb",
	})
	if err != nil && strings.Contains(err.Error(), "missing dbname") {
		t.Fatalf("dbname should be taken from DSN path, got %v", err)
	}
}
