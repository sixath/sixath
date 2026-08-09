package executor

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sixath/framework/datasource"
)

func TestBindNamed(t *testing.T) {
	sql, args, err := bindNamed(
		"SELECT * FROM users WHERE id = :user_id AND status = :status",
		map[string]any{"user_id": 123, "status": "active"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM users WHERE id = ? AND status = ?"
	if sql != want {
		t.Fatalf("sql = %q, want %q", sql, want)
	}
	if len(args) != 2 || args[0] != 123 || args[1] != "active" {
		t.Fatalf("args = %v", args)
	}
}

func TestBindNamed_SkipsStringLiteral(t *testing.T) {
	sql, args, err := bindNamed(
		"SELECT ':user_id' AS x WHERE id = :id",
		map[string]any{"id": 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT ':user_id' AS x WHERE id = ?" {
		t.Fatalf("sql = %q", sql)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v", args)
	}
}

func TestMySQLReader_PositionalParams(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT id FROM users WHERE name = \\?").
		WithArgs("'; DROP TABLE users; --").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ex := NewMySQLExecutor(reg)
	_, err = ex.Query(context.Background(), "ds1",
		"SELECT id FROM users WHERE name = ?",
		QueryOptions{PositionalParams: []any{"'; DROP TABLE users; --"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
