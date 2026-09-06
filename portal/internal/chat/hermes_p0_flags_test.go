package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestEnrichHermesP0FromEnv(t *testing.T) {
	t.Setenv("SATH_TODO_ENABLED", "true")
	t.Setenv("SATH_WEB_TOOLS_ENABLED", "1")
	t.Setenv("SATH_BROWSER_ENABLED", "yes")
	t.Setenv("SATH_AGENT_MEMORY_WRITE_ENABLED", "true")
	t.Setenv("SATH_MEMORY_WRITE_ENABLED", "true") // legacy: must be ignored
	flags := HermesP0ToolFlags{}
	EnrichHermesP0FromEnv(&flags)
	if !flags.TodoEnabled || !flags.WebToolsEnabled || !flags.BrowserEnabled {
		t.Fatalf("flags: %+v", flags)
	}
	if !flags.MemoryWriteEnabled {
		t.Fatal("expected SATH_AGENT_MEMORY_WRITE_ENABLED to enable memory write")
	}
}

func TestEnrichHermesP0FromEnv_LegacyMemoryWriteIgnored(t *testing.T) {
	t.Setenv("SATH_AGENT_MEMORY_WRITE_ENABLED", "")
	t.Setenv("SATH_MEMORY_WRITE_ENABLED", "true")
	flags := HermesP0ToolFlags{}
	EnrichHermesP0FromEnv(&flags)
	if flags.MemoryWriteEnabled {
		t.Fatal("SATH_MEMORY_WRITE_ENABLED must no longer enable writes")
	}
}

func TestRegisterAgentRuntimeTools_DefaultFlagsSkipP0Tools(t *testing.T) {
	reg := tool.NewRegistry()
	SetHermesP0ToolFlags(HermesP0ToolFlags{SkillManageConfirmCreateDelete: true})
	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"todo", "memory", "memory_search", "session_search", "read_file", "web_search", "terminal", "cronjob", "skills_list", "skill_manage", "browser_navigate", "knowledge_search", "knowledge_read"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("expected %q absent with default flags", name)
		}
	}
	for _, name := range []string{"memory_remember", "memory_recall", "memory_get"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("expected %q registered", name)
		}
	}
}

func TestRegisterAgentRuntimeTools_AllFlagsEnabled(t *testing.T) {
	reg := tool.NewRegistry()
	flags := HermesP0ToolFlags{
		MemoryWriteEnabled:        true,
		SkillRuntimeManageEnabled: true,
		TodoEnabled:               true,
		WorkspaceFilesEnabled:     true,
		WebToolsEnabled:           true,
		TerminalLocalEnabled:      true,
		CronjobToolEnabled:        true,
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsIdx, err := BuildSkillsIndex(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{Flags: &flags, SkillsIdx: skillsIdx}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"todo", "memory_remember", "memory_recall", "memory_get", "read_file", "web_search", "terminal", "cronjob", "skills_list", "skill_manage"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("expected %q registered", name)
		}
	}
	for _, name := range []string{"knowledge_search", "knowledge_read", "knowledge_write", "knowledge_approve"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("expected hub knowledge tool %q absent", name)
		}
	}
	for _, name := range []string{"memory", "memory_search", "session_search"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("expected legacy tool %q absent", name)
		}
	}
}

func TestEnvTruthy(t *testing.T) {
	_ = os.Getenv("PATH") // ensure env works
	if envTruthy("SATH_NONEXISTENT_FLAG_XYZ") {
		t.Fatal("missing env should be false")
	}
}
