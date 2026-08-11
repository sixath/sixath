package mention

import (
	"testing"
)

func TestParse_LongestNameFirst(t *testing.T) {
	cands := []Candidate{
		{ID: "id-ops", Name: "ops"},
		{ID: "id-ops-bot", Name: "ops-bot"},
	}
	got := Parse("@ops-bot hello", cands)
	if !got.Hit || got.AgentID != "id-ops-bot" || got.Stripped != "hello" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParse_UUID(t *testing.T) {
	id := "e8107fb3-e40a-4207-9d9a-6768847aaf79"
	cands := []Candidate{{ID: id, Name: "zone"}}
	got := Parse("@"+id+" please check", cands)
	if !got.Hit || got.AgentID != id || got.Stripped != "please check" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParse_CaseInsensitive(t *testing.T) {
	cands := []Candidate{{ID: "a1", Name: "OpsBot"}}
	got := Parse("@opsbot ping", cands)
	if !got.Hit || got.AgentID != "a1" || got.Stripped != "ping" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParse_UnknownMention_NoHit(t *testing.T) {
	cands := []Candidate{{ID: "a1", Name: "ops"}}
	text := "@foo hello"
	got := Parse(text, cands)
	if got.Hit || got.Stripped != text {
		t.Fatalf("got=%+v", got)
	}
}

func TestParse_FirstOnly(t *testing.T) {
	cands := []Candidate{
		{ID: "a", Name: "alpha"},
		{ID: "b", Name: "beta"},
	}
	got := Parse("@alpha then @beta", cands)
	if !got.Hit || got.AgentID != "a" || got.Stripped != "then @beta" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParse_NoAt(t *testing.T) {
	cands := []Candidate{{ID: "a1", Name: "ops"}}
	got := Parse("plain text", cands)
	if got.Hit || got.Stripped != "plain text" {
		t.Fatalf("got=%+v", got)
	}
}
