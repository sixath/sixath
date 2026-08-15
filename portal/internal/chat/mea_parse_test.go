package chat

import (
	"testing"
)

func TestParseMEAChecks_OK(t *testing.T) {
	raw := "create out.txt with hello\n\n```mea-checks\n" +
		`[{"type":"path_exists","path":"out.txt"},{"type":"file_contains","path":"out.txt","pattern":"hello"}]` +
		"\n```\n"
	clean, checks, ok := ParseMEAChecks(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if clean != "create out.txt with hello" {
		t.Fatalf("clean=%q", clean)
	}
	if len(checks) != 2 || checks[0].Type != "path_exists" || checks[1].Pattern != "hello" {
		t.Fatalf("%+v", checks)
	}
}

func TestParseMEAChecks_NoFence(t *testing.T) {
	clean, checks, ok := ParseMEAChecks("just chat")
	if ok || checks != nil || clean != "just chat" {
		t.Fatalf("clean=%q ok=%v checks=%v", clean, ok, checks)
	}
}

func TestParseMEAChecks_BadJSON(t *testing.T) {
	raw := "goal\n```mea-checks\n{not json}\n```"
	clean, _, ok := ParseMEAChecks(raw)
	if ok {
		t.Fatal("expected not ok")
	}
	if clean != "goal" {
		t.Fatalf("clean=%q", clean)
	}
}

func TestParseMEAChecks_EmptyTypesDropped(t *testing.T) {
	raw := "g\n```mea-checks\n[{\"type\":\"\",\"path\":\"x\"},{\"type\":\"path_exists\",\"path\":\"a\"}]\n```"
	_, checks, ok := ParseMEAChecks(raw)
	if !ok || len(checks) != 1 || checks[0].Type != "path_exists" {
		t.Fatalf("%+v ok=%v", checks, ok)
	}
}

func TestParseMEAAcceptance_OK(t *testing.T) {
	raw := "summarize the report\n\n```mea-acceptance\n" +
		`["summary is grounded","no contradictions"]` +
		"\n```\n"
	clean, acceptance, ok := ParseMEAAcceptance(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if clean != "summarize the report" {
		t.Fatalf("clean=%q", clean)
	}
	if len(acceptance) != 2 || acceptance[0] != "summary is grounded" {
		t.Fatalf("%+v", acceptance)
	}
}

func TestParseMEAAcceptance_BadJSON(t *testing.T) {
	raw := "goal\n```mea-acceptance\n{not array}\n```"
	clean, _, ok := ParseMEAAcceptance(raw)
	if ok {
		t.Fatal("expected not ok")
	}
	if clean != "goal" {
		t.Fatalf("clean=%q", clean)
	}
}
