package memory

import (
	"os"
	"strings"
	"testing"
)

func TestProceduralCommitGoRemoved(t *testing.T) {
	if _, err := os.Stat("procedural_commit.go"); err == nil {
		t.Fatal("procedural_commit.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestProceduralBindingGoRemoved(t *testing.T) {
	if _, err := os.Stat("procedural_binding.go"); err == nil {
		t.Fatal("procedural_binding.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestProceduralCatalogGoRemoved(t *testing.T) {
	if _, err := os.Stat("procedural_catalog.go"); err == nil {
		t.Fatal("procedural_catalog.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestPrefetchBackendGo_omitsProceduralBindings(t *testing.T) {
	b, err := os.ReadFile("store_prefetch_backend.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"ProceduralBindings", "LoadPersistedProcedural", "MatchProceduralBindings"} {
		if strings.Contains(src, needle) {
			t.Errorf("prefetch backend must not contain %s", needle)
		}
	}
}
