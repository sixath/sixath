package growth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AppendErrorLearning appends a Status:pending error entry to workspace/.learnings/ERRORS.md
// (or the first discovered learnings dir). Used by append_learning tool and FailureCaptureHook.
func AppendErrorLearning(workspace, summary, details, area string) error {
	return AppendLearning(workspace, "errors", "error", summary, details, area)
}

// AppendLearning appends a structured learning block under LEARNINGS.md or ERRORS.md.
func AppendLearning(workspace, target, category, summary, details, area string) error {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return fmt.Errorf("append_learning: workspace is empty")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("append_learning: summary is required")
	}
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		target = "learnings"
	}
	filename := "LEARNINGS.md"
	if target == "errors" {
		filename = "ERRORS.md"
	}
	category = strings.TrimSpace(category)
	if category == "" {
		if target == "errors" {
			category = "error"
		} else {
			category = "insight"
		}
	}
	dir, err := resolveOrCreateLearningsDir(workspace)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, filename)
	block := FormatLearningBlock(category, summary, details, area)
	return appendLearningFile(path, block)
}

func resolveOrCreateLearningsDir(workspace string) (string, error) {
	dirs := DiscoverLearningsDirs(workspace)
	if len(dirs) > 0 {
		return dirs[0], nil
	}
	dir := filepath.Join(workspace, ".learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("append_learning: mkdir %q: %w", dir, err)
	}
	return dir, nil
}

// FormatLearningBlock builds a markdown learning/error entry (Status: pending).
func FormatLearningBlock(category, summary, details, area string) string {
	id := time.Now().Format("20060102-150405")
	now := time.Now().Format(time.RFC3339)
	var b strings.Builder
	b.WriteString("\n---\n\n")
	b.WriteString(fmt.Sprintf("## [LRN-%s] %s\n\n", id, category))
	b.WriteString("**Logged**: " + now + "\n")
	b.WriteString("**Status**: pending\n")
	if area != "" {
		b.WriteString("**Area**: " + area + "\n")
	}
	b.WriteString("\n### Summary\n")
	b.WriteString(summary + "\n")
	if details != "" {
		b.WriteString("\n### Details\n")
		b.WriteString(details + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func appendLearningFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		header := "# Learnings\n\nCorrections, insights, and knowledge captured by the agent.\n"
		if strings.HasSuffix(strings.ToLower(filepath.Base(path)), "errors.md") {
			header = "# Errors\n\nError patterns and resolutions captured by the agent.\n"
		}
		if _, err := f.WriteString(header); err != nil {
			return err
		}
	}
	_, err = f.WriteString(content)
	return err
}
