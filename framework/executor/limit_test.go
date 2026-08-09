package executor

import (
	"encoding/json"
	"testing"
)

func TestHasLimitClause(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM users LIMIT 10", true},
		{"SELECT * FROM users LIMIT 10, 5", true},
		{"SELECT * FROM users LIMIT 10 ;", true},
		{"SELECT * FROM users LIMIT 10 OFFSET 20", true},
		{"select * from t limit 5", true},
		{"SELECT * FROM users", false},
		{"SELECT 'LIMIT 10' AS x FROM users", false},
		{"SELECT * FROM users WHERE note = 'use LIMIT carefully'", false},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := hasLimitClause(tt.sql); got != tt.want {
				t.Fatalf("hasLimitClause(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestApplyMaxRowsToSQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		maxRows int
		want    string
		wantErr bool
	}{
		{
			name:    "wrap without limit",
			sql:     "SELECT * FROM users",
			maxRows: 10,
			want:    "SELECT * FROM (SELECT * FROM users) AS _limited LIMIT 10",
		},
		{
			name:    "existing limit unchanged",
			sql:     "SELECT * FROM users LIMIT 5",
			maxRows: 10,
			want:    "SELECT * FROM users LIMIT 5",
		},
		{
			name:    "maxRows zero",
			sql:     "SELECT * FROM users",
			maxRows: 0,
			want:    "SELECT * FROM users",
		},
		{
			name:    "for update blocked",
			sql:     "SELECT * FROM users FOR UPDATE",
			maxRows: 10,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyMaxRowsToSQL(tt.sql, tt.maxRows)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInjectESSearchSize(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		maxRows int
		wantSz  int
	}{
		{"inject when missing", `{"query":{"match_all":{}}}`, 10, 10},
		{"clamp larger size", `{"size":500,"query":{"match_all":{}}}`, 10, 10},
		{"keep smaller size", `{"size":5,"query":{"match_all":{}}}`, 10, 5},
		{"maxRows zero", `{"query":{"match_all":{}}}`, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := injectESSearchSize(tt.body, tt.maxRows)
			if err != nil {
				t.Fatalf("inject: %v", err)
			}
			if tt.maxRows == 0 {
				if out != tt.body {
					t.Fatalf("body changed: %s", out)
				}
				return
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(out), &m); err != nil {
				t.Fatalf("parse out: %v", err)
			}
			sz, ok := jsonNumberAsInt(m["size"])
			if !ok {
				t.Fatalf("size missing in %s", out)
			}
			if sz != tt.wantSz {
				t.Fatalf("size = %d, want %d (body %s)", sz, tt.wantSz, out)
			}
		})
	}
}
