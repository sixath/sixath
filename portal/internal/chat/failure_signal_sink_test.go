package chat

import (
	"testing"

	"github.com/sixath/framework/memory"
)

func TestDefaultFailureSignalSink_NonNil(t *testing.T) {
	s := DefaultFailureSignalSink()
	if s == nil {
		t.Fatal("expected non-nil sink")
	}
	_ = memory.FailureSignalSink(s)
}
