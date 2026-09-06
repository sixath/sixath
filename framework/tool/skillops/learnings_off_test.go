package toolskill

import (
	"os"
	"testing"
)

func TestLearningsToolsGoRemoved(t *testing.T) {
	_, err := os.Stat("learnings_tools.go")
	if err == nil {
		t.Fatal("learnings_tools.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
