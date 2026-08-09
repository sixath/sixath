package tool

import (
	"testing"
)

func TestValidateOutboundURL_RejectsNonHTTP(t *testing.T) {
	if err := ValidateOutboundURL("file:///etc/passwd"); err == nil {
		t.Fatal("expected scheme rejection")
	}
}
