package service

import (
	"strings"
	"testing"

	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/model"
)

func TestMessagesForMEAContract_ReplacesLastUser(t *testing.T) {
	base := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old"},
	}
	out := messagesForMEAContract(base, mea.Contract{
		Goal: "new goal",
		AcceptanceChecks: []mea.AcceptanceCheck{
			{Type: "path_exists", Path: "a.txt"},
		},
	})
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Content != "sys" {
		t.Fatal(out[0].Content)
	}
	if out[1].Role != "user" || out[1].Content == "old" {
		t.Fatalf("%q", out[1].Content)
	}
	if !strings.Contains(out[1].Content, "new goal") ||
		!strings.Contains(out[1].Content, "path_exists") ||
		!strings.Contains(out[1].Content, "a.txt") {
		t.Fatalf("%q", out[1].Content)
	}
	if base[1].Content != "old" {
		t.Fatal("base mutated")
	}
}

func TestMessagesForMEAContract_TextAcceptance(t *testing.T) {
	base := []model.Message{{Role: "user", Content: "old"}}
	out := messagesForMEAContract(base, mea.Contract{
		Goal:       "summarize",
		Acceptance: []string{"grounded in evidence"},
	})
	if len(out) != 1 || !strings.Contains(out[0].Content, "grounded in evidence") {
		t.Fatalf("%q", out[0].Content)
	}
}
