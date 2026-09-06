package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sixath/framework/datasource"
)

func TestEvalGolden_deny_write(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ex := NewMySQLExecutor(reg)

	_, err = ex.Execute(context.Background(), "ds1", "INSERT INTO t (a) VALUES (1)", ExecuteOptions{})
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Fatalf("zero-value must deny write, got %v", err)
	}

	mock.ExpectExec("INSERT").WillReturnResult(sqlmock.NewResult(1, 1))
	_, err = ex.Execute(context.Background(), "ds1", "INSERT INTO t (a) VALUES (1)", ExecuteOptions{AllowWrite: true})
	if err != nil {
		t.Fatalf("AllowWrite opt-in must still work: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
