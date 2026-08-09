package chat

import (
	"strings"
	"testing"
)

func TestMemoryFenceStreamScrubber_SplitChunksHidesInner(t *testing.T) {
	tag := "sixath-memory-context"
	open := `<` + tag + ` id="abc12345">`
	close := `</` + tag + `>`
	s := NewMemoryFenceStreamScrubber(tag)
	var ui strings.Builder
	chunks := []string{"Hello ", open, "SECRET", close, " world"}
	for _, c := range chunks {
		ui.WriteString(s.Feed(c))
	}
	tail, trunc := s.Flush()
	if trunc {
		t.Fatal("unexpected eof truncated")
	}
	ui.WriteString(tail)
	got := ui.String()
	if strings.Contains(got, "SECRET") {
		t.Fatalf("UI leaked fence body: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Fatalf("expected outside text preserved, got %q", got)
	}
}

func TestMemoryFenceStreamScrubber_EOFInsideFenceTruncates(t *testing.T) {
	tag := "sixath-memory-context"
	open := `<` + tag + ` id="deadbeef">`
	s := NewMemoryFenceStreamScrubber(tag)
	_ = s.Feed("x" + open + "never-closed")
	tail, trunc := s.Flush()
	if !trunc {
		t.Fatal("expected eof inside fence")
	}
	if tail != "" {
		t.Fatalf("expected no tail after truncated fence, got %q", tail)
	}
}

func TestMemoryFenceStreamScrubber_WrongCloseIDStaysInside(t *testing.T) {
	tag := "sixath-memory-context"
	s := NewMemoryFenceStreamScrubber(tag)
	_ = s.Feed(`<` + tag + ` id="aaa">`)
	_ = s.Feed(`inner</` + tag + ` id="bbb">`)
	tail, trunc := s.Flush()
	if !trunc {
		t.Fatalf("expected truncated, tail=%q", tail)
	}
}

func TestMemoryFenceStreamScrubber_CloseWithMatchingID(t *testing.T) {
	tag := "sixath-memory-context"
	s := NewMemoryFenceStreamScrubber(tag)
	out := s.Feed(`<` + tag + ` id="x">` + "IN" + `</` + tag + ` id="x">` + "OK")
	if strings.Contains(out, "IN") || !strings.Contains(out, "OK") {
		t.Fatalf("got %q", out)
	}
}
