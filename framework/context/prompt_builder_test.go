package context

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestBuild_ToolNamesOrderIndependent(t *testing.T) {
	a := Build(Input{AgentSystem: "sys", ToolNames: []string{"b", "a", "a"}})
	b := Build(Input{AgentSystem: "sys", ToolNames: []string{"a", "b"}})
	if a.Stable != b.Stable || a.StableHash != b.StableHash {
		t.Fatalf("stable/hash must be order-independent\nA=%q\nB=%q\nAh=%s Bh=%s", a.Stable, b.Stable, a.StableHash, b.StableHash)
	}
	if !strings.Contains(a.Stable, "## Tools\n- a\n- b") {
		t.Fatalf("tools block: %q", a.Stable)
	}
}

func TestBuild_EmptyBlocksOmitted(t *testing.T) {
	r := Build(Input{AgentSystem: "hello"})
	if strings.Contains(r.Stable, "## Skills") || strings.Contains(r.Stable, "## MEMORY.md") ||
		strings.Contains(r.Stable, "## USER.md") || strings.Contains(r.Stable, "## Tools") {
		t.Fatalf("empty blocks must be omitted: %q", r.Stable)
	}
	if r.Stable != "hello" {
		t.Fatalf("agent text only: got %q", r.Stable)
	}
}

func TestBuild_AgentTextHasNoHeading(t *testing.T) {
	r := Build(Input{
		AgentSystem: "You are RCA.",
		SkillsIndex: "idx",
		MemoryMD:    "mem",
		UserMD:      "usr",
		ToolNames:   []string{"grep"},
	})
	if strings.HasPrefix(r.Stable, "#") {
		t.Fatalf("agent text must be unheaded: %q", r.Stable)
	}
	if !strings.HasPrefix(r.Stable, "You are RCA.\n\n## Skills\nidx\n\n## MEMORY.md\nmem\n\n## USER.md\nusr\n\n## Tools\n- grep") {
		t.Fatalf("block order: %q", r.Stable)
	}
}

func TestBuild_WhitespaceOnlyBlocksOmitted(t *testing.T) {
	r := Build(Input{
		AgentSystem: "sys",
		SkillsIndex: "  \n",
		MemoryMD:    "\t",
		UserMD:      " ",
		ToolNames:   []string{"", "  "},
	})
	if strings.Contains(r.Stable, "##") {
		t.Fatalf("whitespace blocks must be omitted: %q", r.Stable)
	}
}

func TestBuild_StableHashIsSHA256Prefix(t *testing.T) {
	r := Build(Input{AgentSystem: "sys"})
	sum := sha256.Sum256([]byte(r.Stable))
	want := hex.EncodeToString(sum[:])[:16]
	if r.StableHash != want {
		t.Fatalf("hash=%s want=%s", r.StableHash, want)
	}
	if len(r.StableHash) != 16 {
		t.Fatalf("hash len=%d", len(r.StableHash))
	}
}

func TestBuild_EphemeralDoesNotAffectHash(t *testing.T) {
	a := Build(Input{AgentSystem: "sys", Ephemeral: ""})
	b := Build(Input{AgentSystem: "sys", Ephemeral: "budget warning"})
	if a.Stable != b.Stable || a.StableHash != b.StableHash {
		t.Fatal("ephemeral must not change stable/hash")
	}
	if b.Ephemeral != "budget warning" {
		t.Fatalf("ephemeral passthrough: %q", b.Ephemeral)
	}
}

func TestEncode_NoEphemeralOmitsSeparator(t *testing.T) {
	got := Encode("stable body", "")
	if got != "stable body" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "---") {
		t.Fatal("must not contain ---")
	}
	if Encode("stable body", "  \n") != "stable body" {
		t.Fatal("whitespace ephemeral is empty")
	}
}

func TestEncode_WithEphemeral(t *testing.T) {
	got := Encode("stable body", "warn")
	if got != "stable body\n\n---\n\nwarn" {
		t.Fatalf("got %q", got)
	}
}
