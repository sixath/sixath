package tooldata

import (
	"os"
	"testing"
)

func TestSQLHealGoRemoved(t *testing.T) {
	if _, err := os.Stat("sql_heal.go"); err == nil {
		t.Fatal("unused HealReadSQL must be removed after default execute_read stopped auto-heal")
	}
}
