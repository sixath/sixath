package executor

import (
	"errors"
	"testing"
)

func TestIsWriteDSL(t *testing.T) {
	tests := []struct {
		dsl   string
		write bool
	}{
		{"SELECT 1", false},
		{"  select * from t", false},
		{"SHOW TABLES", false},
		{"DESCRIBE users", false},
		{"EXPLAIN SELECT 1", false},
		{"INSERT INTO t VALUES (1)", true},
		{"  update t set a=1", true},
		{"delete from t", true},
		{"REPLACE INTO t VALUES (1)", true},
		{"CREATE TABLE t (id int)", true},
		{"DROP TABLE t", true},
		{"ALTER TABLE t ADD c int", true},
		{"TRUNCATE t", true},
		{"", false},
		{"/* hint */ DELETE FROM t", true},
		{"-- comment\nDELETE FROM t", true},
		{"WITH cte AS (SELECT 1) DELETE FROM t USING cte", true},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"CALL sp_delete()", true},
		{"LOAD DATA INFILE 'x' INTO TABLE t", true},
		{"LOCK TABLES t WRITE", true},
		{"SELECT * FROM t WHERE note='DELETE is ok'", false},
	}
	for _, tt := range tests {
		t.Run(tt.dsl, func(t *testing.T) {
			got := isWriteDSL(tt.dsl)
			if got != tt.write {
				t.Errorf("isWriteDSL(%q) = %v, want %v", tt.dsl, got, tt.write)
			}
		})
	}
}

func TestPrepareSQL_MultiStatement(t *testing.T) {
	_, err := prepareSQL("SELECT 1; DELETE FROM t")
	if !errors.Is(err, ErrUnsupportedSyntax) {
		t.Fatalf("expected ErrUnsupportedSyntax, got %v", err)
	}
}

func TestPrepareSQL_StringLiteralComment(t *testing.T) {
	got, err := prepareSQL("SELECT '/* not comment */' AS x")
	if err != nil {
		t.Fatal(err)
	}
	if isWriteSQL(got) {
		t.Fatalf("expected read SQL, got %q", got)
	}
}

func FuzzIsWriteDSL(f *testing.F) {
	f.Add("SELECT 1")
	f.Add("/* x */ DELETE FROM t")
	f.Fuzz(func(t *testing.T, dsl string) {
		_ = isWriteDSL(dsl)
	})
}
