package conf

import (
	"testing"
)

func TestEnrichDataFromEnv_fullDSN(t *testing.T) {
	t.Setenv("SATH_MYSQL_DSN", "root:x@tcp(mysql:3306)/sath?parseTime=True")
	t.Setenv("SATH_MYSQL_PASSWORD", "ignored")
	d := &Data{Database: &Data_Database{Source: "root:old@tcp(mysql:3306)/sath"}}
	EnrichDataFromEnv(d)
	if d.Database.Source != "root:x@tcp(mysql:3306)/sath?parseTime=True" {
		t.Fatalf("source=%q", d.Database.Source)
	}
}

func TestEnrichDataFromEnv_passwordReplace(t *testing.T) {
	t.Setenv("SATH_MYSQL_DSN", "")
	t.Setenv("SATH_MYSQL_PASSWORD", "newpass")
	d := &Data{Database: &Data_Database{
		Source: "root:root@tcp(mysql:3306)/sath?parseTime=True&loc=Local&charset=utf8mb4",
	}}
	EnrichDataFromEnv(d)
	want := "root:newpass@tcp(mysql:3306)/sath?parseTime=True&loc=Local&charset=utf8mb4"
	if d.Database.Source != want {
		t.Fatalf("source=%q, want %q", d.Database.Source, want)
	}
}

func TestReplaceMySQLDSNPassword(t *testing.T) {
	got := replaceMySQLDSNPassword("user:old@tcp(h:3306)/db", "p@ss")
	if got != "user:p@ss@tcp(h:3306)/db" {
		t.Fatalf("got %q", got)
	}
}
