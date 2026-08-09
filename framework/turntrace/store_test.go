package turntrace

import (
	"context"
	"testing"
)

func TestNoopExporter(t *testing.T) {
	if err := (NoopExporter{}).Export(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
