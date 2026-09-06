package tool

import (
	"os"
	"testing"
)

func TestHyperToolGoRemoved(t *testing.T) {
	if _, err := os.Stat("hypertool.go"); err == nil {
		t.Fatal("hypertool.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
