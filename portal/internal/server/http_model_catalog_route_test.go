package server

import (
	"os"
	"strings"
	"testing"
)

func TestHTTP_KeepsModelCatalogRoutes(t *testing.T) {
	b, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"/model-providers",
		"/model-choices",
		"/sessions/{session_id}/model",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing route %s", want)
		}
	}
}
