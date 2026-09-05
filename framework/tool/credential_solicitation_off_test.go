package tool

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogSearchGo_omitsPlainTextCredentialRedirect(t *testing.T) {
	b, err := os.ReadFile("catalog_search.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{
		"MatchCredentialSolicitation",
		"FormatCredentialSolicitationRedirect",
	} {
		if strings.Contains(src, needle) {
			t.Errorf("catalog_search.go must not define %s", needle)
		}
	}
}
