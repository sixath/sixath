package tool

import (
	"path/filepath"
	"strings"
)

// ExtractControlFlow returns AST path tables for functions overlapping
// [startLine, endLine]. Unknown languages and parse failures return nil (fail-open).
// Additional languages should plug in here and emit the same ControlFlowFunc JSON.
func ExtractControlFlow(src []byte, file string, startLine, endLine int) []ControlFlowFunc {
	switch controlFlowLanguage(file) {
	case "go":
		return extractGoControlFlow(src, file, startLine, endLine)
	default:
		return nil
	}
}

func controlFlowLanguage(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".go":
		return "go"
	default:
		return ""
	}
}
