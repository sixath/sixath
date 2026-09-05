package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrowthPackageRemoved(t *testing.T) {
	_, err := os.Stat(filepath.Join("..", "growth"))
	if err == nil {
		t.Fatal("framework/growth must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
