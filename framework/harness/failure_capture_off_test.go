package harness

import (
	"os"
	"strings"
	"testing"
)

func TestFailureCaptureHookFileRemoved(t *testing.T) {
	_, err := os.Stat("failure_capture_hook.go")
	if err == nil {
		t.Fatal("failure_capture_hook.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestHarnessDoesNotDefineFailureCaptureHook(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	needle := "New" + "FailureCaptureHook"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), needle) {
			t.Errorf("%s must not define the FailureCapture constructor", e.Name())
		}
	}
}
