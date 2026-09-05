package templates

import (
	"os"
	"strings"
	"testing"
)

func TestSkillsHandlerGo_doesNotWireHyperTool(t *testing.T) {
	b, err := os.ReadFile("skills_handler.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, "RegisterHyperTool") {
		t.Fatal("default skills handler must not RegisterHyperTool")
	}
	if strings.Contains(src, "HyperToolPromptSnippet") {
		t.Fatal("default skills handler must not inject HyperToolPromptSnippet")
	}
}
