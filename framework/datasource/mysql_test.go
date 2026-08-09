package datasource

import (
	"strings"
	"testing"
)

func TestBuildMySQLDSN_NoPasswordInOpenError(t *testing.T) {
	cfg := Config{
		ID:     "ds1",
		Host:   "127.0.0.1",
		User:   "u",
		DBName: "db",
		Port:   3306,
	}
	dsn, err := buildMySQLDSN(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dsn, "multiStatements=true") {
		t.Fatalf("dsn must not enable multiStatements: %s", dsn)
	}
	if !strings.Contains(dsn, "multiStatements=false") {
		t.Fatalf("dsn must force multiStatements=false: %s", dsn)
	}
}

func TestBuildMySQLDSN_StripsMultiStatementsFromProvidedDSN(t *testing.T) {
	cfg := Config{
		ID:  "ds1",
		DSN: "user:pass@tcp(127.0.0.1:3306)/db?multiStatements=true&parseTime=true",
	}
	dsn, err := buildMySQLDSN(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dsn, "multiStatements=true") {
		t.Fatalf("expected multiStatements stripped: %s", dsn)
	}
}

func TestNewMySQLDataSource_ParseDSNErrorOmitsPassword(t *testing.T) {
	_, err := NewMySQLDataSource(Config{ID: "ds1", DSN: "user:s3cret@tcp([bad"})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error must not contain password: %v", err)
	}
}
