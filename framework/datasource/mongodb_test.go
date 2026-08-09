package datasource

import (
	"strings"
	"testing"
)

func TestBuildMongoURI_EscapesAtInPassword(t *testing.T) {
	uri := buildMongoURI("mongo.example.com", 27017, "admin", "p@ss:w#rd", "migu")
	if strings.Contains(uri, "p@ss") {
		t.Fatalf("password @ must be escaped, got %q", uri)
	}
	if !strings.Contains(uri, "%40") {
		t.Fatalf("expected %%40 in URI, got %q", uri)
	}
	if !strings.HasSuffix(uri, "/migu?readPreference=secondaryPreferred") && !strings.Contains(uri, "/migu?") {
		t.Fatalf("unexpected path/query: %q", uri)
	}
}
