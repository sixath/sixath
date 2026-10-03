package tooldata

import (
	"reflect"
	"testing"
)

func TestParseSingleTableSQL(t *testing.T) {
	cases := []struct {
		sql       string
		table     string
		qualified bool
		cols      []string
		hasCond   bool
		ok        bool
	}{
		{"SELECT id, state FROM vm_assign WHERE state = 3 ORDER BY id DESC LIMIT 10", "vm_assign", false, []string{"id", "state"}, true, true},
		{"select * from `pool_config` where image_version like '1.%'", "pool_config", false, []string{"image_version"}, true, true},
		{"SELECT COUNT(*) FROM t WHERE a.b = 1 AND c IN (1,2) AND d IS NULL", "t", false, []string{"b", "c", "d"}, true, true},
		{"SELECT name FROM db1.users", "db1.users", true, []string{"name"}, false, true},
		{"SELECT state, COUNT(*) cnt FROM t GROUP BY state ORDER BY cnt DESC", "t", false, []string{"state"}, false, true},
		{"SELECT state AS s FROM t ORDER BY s", "t", false, nil, false, true},
		{"SELECT x.id FROM vm_assign x WHERE x.state = 1", "vm_assign", false, []string{"id", "state"}, true, true},
		{"SELECT a FROM t1 JOIN t2 ON t1.id = t2.id", "", false, nil, false, false},
		{"SELECT a FROM (SELECT a FROM t) x", "", false, nil, false, false},
		{"WITH x AS (SELECT 1) SELECT * FROM x", "", false, nil, false, false},
		{"SHOW TABLES", "", false, nil, false, false},
		{"SELECT a FROM t WHERE note = 'x = y'", "t", false, []string{"a", "note"}, true, true},
	}
	for _, tc := range cases {
		got, ok := parseSingleTableSQL(tc.sql)
		if ok != tc.ok {
			t.Fatalf("%q ok=%v", tc.sql, ok)
		}
		if !ok {
			continue
		}
		if got.Table != tc.table || got.Qualified != tc.qualified || !reflect.DeepEqual(got.Columns, tc.cols) || got.HasCondition != tc.hasCond {
			t.Errorf("%q => %+v", tc.sql, got)
		}
	}
}

func TestNonSQLReason(t *testing.T) {
	for _, s := range []string{"level:ERROR AND service:foo", `{"query":{"match_all":{}}}`, "service:foo"} {
		if _, hit := nonSQLReason(s); !hit {
			t.Errorf("%q should be rejected", s)
		}
	}
	for _, s := range []string{"SELECT 1", "  show tables", "DESC t", "WITH x AS (SELECT 1) SELECT * FROM x", "(SELECT 1)", "explain select 1",
		"/* q1 */ SELECT a FROM t WHERE x = 1 AND y = 2", "-- note\nSELECT a FROM t WHERE x = 1 AND y = 2", ""} {
		if r, hit := nonSQLReason(s); hit {
			t.Errorf("%q wrongly rejected: %s", s, r)
		}
	}
}

func TestQuoteSQLTable(t *testing.T) {
	if got := quoteSQLTable("db1.users"); got != "`db1`.`users`" {
		t.Fatalf("got %s", got)
	}
}
