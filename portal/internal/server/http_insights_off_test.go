package server

import (
	"os"
	"strings"
	"testing"
)

func TestHTTP_OmitsInsightsRoute(t *testing.T) {
	b, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "/insights") {
		t.Fatal("insights must not be registered on the HTTP shell")
	}
}

func TestHTTP_KeepsRewindRoute(t *testing.T) {
	b, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/rewind") {
		t.Fatal("rewind route must stay")
	}
}
