package redact

import (
	"reflect"
	"testing"
)

func TestString(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"curl user", "curl -u admin:s3cret http://x", "curl -u admin:*** http://x"},
		{"curl user quoted", "curl -u 'admin:s3cret' http://x", "curl -u 'admin:***' http://x"},
		{"curl --user", "curl --user=admin:s3cret http://x", "curl --user=admin:*** http://x"},
		{"password kv", "mysql password=abc123 db", "mysql password=*** db"},
		{"passwd colon", "passwd: abc123", "passwd: ***"},
		{"pwd kv", "login pwd=abc", "login pwd=***"},
		{"api_key query", "GET /v1?api_key=XYZ&x=1", "GET /v1?api_key=***&x=1"},
		{"bearer header", "-H 'Authorization: Bearer abc.def'", "-H 'Authorization: Bearer ***'"},
		{"basic header", "Authorization: Basic Zm9vOmJhcg==", "Authorization: Basic ***"},
		{"unknown auth scheme", "Authorization: Token abc123", "Authorization: Token ***"},
		{"auth without scheme", "Authorization: abc123 next", "Authorization: *** next"},
		{"url creds", "ftp://lightplay:pa55@10.0.0.1/a", "ftp://lightplay:***@10.0.0.1/a"},
		{"url creds empty user", "redis://:pass@host:6379", "redis://:***@host:6379"},
		{"sshpass", "sshpass -p 'pa55' ssh root@h", "sshpass -p '***' ssh root@h"},
		{"json password", `{"password":"x"}`, `{"password":"***"}`},
		{"json token spaced", `{"token": "abc"}`, `{"token": "***"}`},
		{"json authorization", `{"Authorization": "Bearer abc"}`, `{"Authorization": "Bearer ***"}`},
		{"prefixed access_token", "access_token=abc", "access_token=***"},
		{"prefixed client_secret", "client_secret=abc&x=1", "client_secret=***&x=1"},
		{"env PGPASSWORD", "PGPASSWORD=x psql", "PGPASSWORD=*** psql"},
		{"env MYSQL_PWD", "MYSQL_PWD=x mysql", "MYSQL_PWD=*** mysql"},
		{"x-api-key header", "x-api-key: abc", "x-api-key: ***"},
		{"aws secret access key", "export AWS_SECRET_ACCESS_KEY=wJalr", "export AWS_SECRET_ACCESS_KEY=***"},
		{"secret_key", "secret_key=x", "secret_key=***"},
		{"private_key colon", "private_key: x", "private_key: ***"},
		{"credentials", "credentials=x", "credentials=***"},
		{"escaped json value", `"message:\"invalid token: expired\""`, `"message:\"invalid token: ***\""`},
		{"plain text untouched", "SELECT * FROM t WHERE max_tokens=5", "SELECT * FROM t WHERE max_tokens=5"},
		{"tokens colon untouched", "input_tokens: 3", "input_tokens: 3"},
		{"url without creds", "http://host:8080/path", "http://host:8080/path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := String(c.in); got != c.want {
				t.Errorf("String(%q)=%q want %q", c.in, got, c.want)
			}
		})
	}
}

func TestValue_Nested(t *testing.T) {
	in := map[string]any{
		"command": "curl -u a:b http://x",
		"headers": map[string]any{"Authorization": "Bearer t", "Accept": "json"},
		"list":    []any{"password=p", 3},
		"count":   float64(2),
	}
	want := map[string]any{
		"command": "curl -u a:*** http://x",
		"headers": map[string]any{"Authorization": Mask, "Accept": "json"},
		"list":    []any{"password=***", 3},
		"count":   float64(2),
	}
	if got := Value(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("Value=%#v\nwant %#v", got, want)
	}
	if in["command"] != "curl -u a:b http://x" {
		t.Fatal("Value must not mutate its input")
	}
}

func TestValue_NestedInputNotMutated(t *testing.T) {
	inner := map[string]any{"password": "p", "cmd": "curl -u a:b http://x"}
	list := []any{"token=t"}
	in := map[string]any{"inner": inner, "list": list}
	Value(in)
	if inner["password"] != "p" || inner["cmd"] != "curl -u a:b http://x" {
		t.Fatalf("nested map mutated: %#v", inner)
	}
	if list[0] != "token=t" {
		t.Fatalf("nested slice mutated: %#v", list)
	}
}

func TestValue_StringMaps(t *testing.T) {
	got := Value(map[string]string{"token": "x", "host": "h"})
	want := map[string]string{"token": Mask, "host": "h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestValue_NilStaysNil(t *testing.T) {
	if got := Value(map[string]any(nil)).(map[string]any); got != nil {
		t.Errorf("nil map[string]any -> %#v", got)
	}
	if got := Value(map[string]string(nil)).(map[string]string); got != nil {
		t.Errorf("nil map[string]string -> %#v", got)
	}
	if got := Value([]any(nil)).([]any); got != nil {
		t.Errorf("nil []any -> %#v", got)
	}
	if got := Value([]string(nil)).([]string); got != nil {
		t.Errorf("nil []string -> %#v", got)
	}
}

func TestSecretKey(t *testing.T) {
	for _, k := range []string{
		"password", "X-Api-Token", "Authorization", "access_token", "cookie",
		"pwd", "credential", "private_key", "access_key", "secret_key", "passphrase", "api-key", "x-api-key",
	} {
		if !SecretKey(k) {
			t.Errorf("%q should be secret", k)
		}
	}
	for _, k := range []string{"max_tokens", "input_tokens", "output_tokens", "host", "command"} {
		if SecretKey(k) {
			t.Errorf("%q should not be secret", k)
		}
	}
}
