package memory

import (
	"os"
	"testing"
)

func TestHubPackageRemoved(t *testing.T) {
	_, err := os.Stat("hub")
	if err == nil {
		t.Fatal("framework/memory/hub must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
