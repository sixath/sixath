package harness

import (
	"os"
	"strings"
	"testing"
)

func TestReActAgentGo_doesNotWireDomainGates(t *testing.T) {
	b, err := os.ReadFile("react_agent.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{
		"credentialSolicitationRedirect",
		"【本轮任务锁】",
		"task_lock_q",
	} {
		if strings.Contains(src, needle) {
			t.Errorf("default ReAct loop must not contain %q", needle)
		}
	}
}
