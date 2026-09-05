package chat

import (
	"os"
	"strings"
	"testing"
)

func TestHubBootstrapFileRemoved(t *testing.T) {
	_, err := os.Stat("hub_bootstrap.go")
	if err == nil {
		t.Fatal("hub_bootstrap.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestChatPackageDoesNotImportHub(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	needle := "github.com/sixath/framework/memory/" + "hub"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), needle) {
			t.Errorf("%s must not import memory/hub", e.Name())
		}
	}
}
