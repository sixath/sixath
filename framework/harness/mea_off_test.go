package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMEAPackageRemoved(t *testing.T) {
	_, err := os.Stat(filepath.Join("..", "mea"))
	if err == nil {
		t.Fatal("framework/mea must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
