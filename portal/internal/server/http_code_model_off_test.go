package server

import (
	"os"
	"strings"
	"testing"
)

func TestHTTP_OmitsCodeModelSettingsRoute(t *testing.T) {
	b, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "/settings/code-model") {
		t.Fatal("code-model settings must not be registered on the HTTP shell")
	}
}
