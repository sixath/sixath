package datasource

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildMongoURI_EscapesAtInPassword(t *testing.T) {
	uri := buildMongoURI("mongo.example.com", 27017, "admin", "p@ss:w#rd", "migu", "admin")
	if strings.Contains(uri, "p@ss") {
		t.Fatalf("password @ must be escaped, got %q", uri)
	}
	if !strings.Contains(uri, "%40") {
		t.Fatalf("expected %%40 in URI, got %q", uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/migu" {
		t.Fatalf("path=%q", u.Path)
	}
	q := u.Query()
	if q.Get("authSource") != "admin" {
		t.Fatalf("authSource=%q want admin in %q", q.Get("authSource"), uri)
	}
	if q.Get("readPreference") != "secondaryPreferred" {
		t.Fatalf("readPreference=%q", q.Get("readPreference"))
	}
}

func TestBuildMongoURI_CustomAuthSource(t *testing.T) {
	uri := buildMongoURI("127.0.0.1", 27017, "app", "secret", "appdb", "appdb")
	if !strings.Contains(uri, "authSource=appdb") {
		t.Fatalf("custom authSource missing: %q", uri)
	}
}

func TestEncodeMongoURIUserinfo(t *testing.T) {
	in := "mongodb://admin:Migu@2233@10.19.240.106:27017/d_union_user_game_area_storage_info"
	got := encodeMongoURIUserinfo(in)
	if strings.Contains(got, "Migu@2233@") {
		t.Fatalf("still unescaped: %q", got)
	}
	if !strings.Contains(got, "Migu%402233") {
		t.Fatalf("expected encoded password, got %q", got)
	}
	if _, err := url.Parse(got); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeMongoURIUserinfo_AlreadyEncoded(t *testing.T) {
	in := "mongodb://admin:Migu%402233@10.19.240.106:27017/admin?authSource=admin"
	if got := encodeMongoURIUserinfo(in); got != in {
		t.Fatalf("rewrote encoded URI: %q", got)
	}
}

func TestEnsureMongoAuthSource(t *testing.T) {
	in := "mongodb://admin:Migu%402233@10.19.240.106:27017/d_union"
	got := ensureMongoAuthSource(in, "admin")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("authSource") != "admin" {
		t.Fatalf("got %q", got)
	}
	same := ensureMongoAuthSource(got, "other")
	if urlQuery(same, "authSource") != "admin" {
		t.Fatalf("must not override existing authSource: %q", same)
	}
}

func urlQuery(raw, key string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func TestMongoAuthSourceDefaultAdmin(t *testing.T) {
	if got := mongoAuthSource(Config{User: "admin", DBName: "app"}); got != "admin" {
		t.Fatalf("got %q", got)
	}
	if got := mongoAuthSource(Config{User: "admin", AuthSource: "appdb"}); got != "appdb" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigFromMap_AuthSource(t *testing.T) {
	got := ConfigFromMap(map[string]interface{}{"auth_source": "admin"})
	if got.AuthSource != "admin" {
		t.Fatalf("auth_source=%q", got.AuthSource)
	}
	got = ConfigFromMap(map[string]interface{}{"authSource": "app"})
	if got.AuthSource != "app" {
		t.Fatalf("authSource=%q", got.AuthSource)
	}
}
