package chat

import (
	"os"
	"strings"
	"testing"
)

func TestAgentBuilderGo_doesNotWirePrefetchOrchestrator(t *testing.T) {
	b, err := os.ReadFile("agent_builder.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "prefetchOrchestratorForReAct") {
		t.Fatal("BuildReActAgent must not inject memory prefetch orchestrator")
	}
}

func TestMcpExpandGo_doesNotKeepToolFamilyIndex(t *testing.T) {
	b, err := os.ReadFile("mcp_expand.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, "ToolFamily") || strings.Contains(src, "toolFamily") {
		t.Fatal("McpExpandOnMiss must not keep TurnIntentGate toolFamily index")
	}
	if strings.Contains(src, "BuildToolFamilyIndex") {
		t.Fatal("McpExpandOnMiss must not refresh BuildToolFamilyIndex")
	}
}
