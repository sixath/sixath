package executor

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/sixath/framework/datasource"
)

type dsWithDB struct {
	id string
	db *sql.DB
}

func (d *dsWithDB) ID() string                     { return d.id }
func (d *dsWithDB) Type() string                   { return datasource.TypeMySQL }
func (d *dsWithDB) Ping(ctx context.Context) error { return nil }
func (d *dsWithDB) Close() error                   { return nil }
func (d *dsWithDB) DB() *sql.DB                    { return d.db }

func TestMySQLExecutor_Execute_ReadOnly_RejectsWrite(t *testing.T) {
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
	ctx := context.Background()
	opts := ExecuteOptions{}

	_, err = ex.Execute(ctx, "ds1", "INSERT INTO t (a) VALUES (1)", opts)
	if err == nil {
		t.Fatal("expected error for INSERT in read-only mode")
	}
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Errorf("expected ErrReadOnlyViolation, got %v", err)
	}

	_, err = ex.Execute(ctx, "ds1", "UPDATE t SET a=1", opts)
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Errorf("expected ErrReadOnlyViolation for UPDATE, got %v", err)
	}

	// SELECT 不应触发写拒绝；mock 不期望任何 DB 调用若我们只测到上面就返回
	// 下面单独测 SELECT 会执行
	mock.ExpectQuery("SELECT 1").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	res, err := ex.Execute(ctx, "ds1", "SELECT 1", opts)
	if err != nil {
		t.Fatalf("SELECT in read-only: %v", err)
	}
	if len(res.Rows) != 1 || len(res.Columns) != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestMySQLExecutor_Execute_Query_MaxRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "a").
		AddRow(2, "b")
	mock.ExpectQuery("SELECT \\* FROM \\(SELECT id, name FROM users\\) AS _limited LIMIT 2").
		WillReturnRows(rows)

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ex := NewMySQLExecutor(reg)
	res, err := ex.Execute(context.Background(), "ds1", "SELECT id, name FROM users", ExecuteOptions{MaxRows: 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "id" || res.Columns[1] != "name" {
		t.Errorf("Columns: %v", res.Columns)
	}
	if len(res.Rows) != 2 {
		t.Errorf("expected 2 rows (MaxRows=2), got %d", len(res.Rows))
	}
	if !res.Truncated {
		t.Error("expected Truncated=true when MaxRows < total rows")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestMySQLExecutor_Execute_Query_NotTruncatedWhenWithinLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2)
	mock.ExpectQuery("SELECT \\* FROM \\(SELECT id FROM users\\) AS _limited LIMIT 10").
		WillReturnRows(rows)

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ex := NewMySQLExecutor(reg)
	res, err := ex.Execute(context.Background(), "ds1", "SELECT id FROM users", ExecuteOptions{MaxRows: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Truncated {
		t.Error("expected Truncated=false when MaxRows >= total rows")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestMySQLExecutor_Execute_Write_AffectedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("UPDATE users SET name").
		WillReturnResult(sqlmock.NewResult(0, 3))

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ex := NewMySQLExecutor(reg)
	res, err := ex.Execute(context.Background(), "ds1", "UPDATE users SET name = 'x'", ExecuteOptions{AllowWrite: true})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.AffectedRows != 3 {
		t.Errorf("AffectedRows = %d, want 3", res.AffectedRows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestMySQLExecutor_Execute_UnsupportedDataSource(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	if _, err := reg.Register(datasource.Config{ID: "noop", Type: "noop"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ex := NewMySQLExecutor(reg)
	_, err := ex.Execute(context.Background(), "noop", "SELECT 1", ExecuteOptions{})
	if err == nil {
		t.Fatal("expected error for datasource without DB()")
	}
	if !errors.Is(err, ErrUnsupportedDataSource) {
		t.Errorf("expected ErrUnsupportedDataSource, got %v", err)
	}
}

func TestMySQLExecutor_Execute_Timeout(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT SLEEP").
		WillDelayFor(100 * 1e9) // 100s delay; context will cancel first
	// 若驱动支持 context 取消，会提前返回；这里仅验证带 Timeout 的 opts 能传下去且不 panic
	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ex := NewMySQLExecutor(reg)
	ctx, cancel := context.WithTimeout(context.Background(), 0) // 立即过期
	defer cancel()
	_, _ = ex.Execute(ctx, "ds1", "SELECT SLEEP(100)", ExecuteOptions{Timeout: 10})
	// 期望因 context 已取消而得到错误（不断言具体错误，仅保证不 panic）
}

func TestMySQLExecutor_PushdownLimit_Integration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT id FROM users LIMIT 5").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	reg := datasource.NewRegistry()
	reg.RegisterType("mysql", func(cfg datasource.Config) (datasource.DataSource, error) {
		return &dsWithDB{id: cfg.ID, db: db}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: "mysql"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ex := NewMySQLExecutor(reg)
	_, err = ex.Execute(context.Background(), "ds1", "SELECT id FROM users LIMIT 5", ExecuteOptions{MaxRows: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

func TestIsMySQLSchemaRelated_TypedErrno(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"unknown column 1054", &mysqldriver.MySQLError{Number: 1054, Message: "Unknown column 'foo'"}, true},
		{"no such table 1146", &mysqldriver.MySQLError{Number: 1146, Message: "Table 'x' doesn't exist"}, true},
		{"unknown table 1051", &mysqldriver.MySQLError{Number: 1051, Message: "Unknown table 'x'"}, true},
		{"bad db 1049", &mysqldriver.MySQLError{Number: 1049, Message: "Unknown database 'x'"}, true},
		{"connection error 2002 NOT schema", &mysqldriver.MySQLError{Number: 2002, Message: "Can't connect"}, false},
		{"plain error WHERE port=1054 NOT schema", errors.New("WHERE port=1054 timed out"), false},
		{"nil err", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMySQLSchemaRelated(tt.err)
			if got != tt.want {
				t.Errorf("isMySQLSchemaRelated(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
