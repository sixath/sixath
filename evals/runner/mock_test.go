package main

import "testing"

func TestMock_SingleToolPasses(t *testing.T) {
	task := Task{ID: "t1", Category: "single_tool", Input: "x", Expect: Expectation{Tools: []string{"list_tables"}}, MaxSteps: 3}
	res := runMockTask(task, buildMockRegistry())
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
	if len(res.ToolsUsed) != 1 || res.ToolsUsed[0] != "list_tables" {
		t.Fatalf("tools = %v", res.ToolsUsed)
	}
}

func TestMock_MultiToolSequence(t *testing.T) {
	task := Task{ID: "t2", Category: "multi_tool", Input: "x", Expect: Expectation{ToolSequence: []string{"list_tables", "execute_read"}}, MaxSteps: 5}
	res := runMockTask(task, buildMockRegistry())
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
	if len(res.ToolsUsed) != 2 || res.ToolsUsed[0] != "list_tables" || res.ToolsUsed[1] != "execute_read" {
		t.Fatalf("tools = %v", res.ToolsUsed)
	}
}

func TestMock_MissingToolFails(t *testing.T) {
	task := Task{ID: "t3", Category: "single_tool", Input: "x", Expect: Expectation{Tools: []string{"nonexistent_tool"}}, MaxSteps: 3}
	res := runMockTask(task, buildMockRegistry())
	if res.Passed {
		t.Fatal("expected fail for missing tool")
	}
	if res.FailureReason != "tool_error" {
		t.Fatalf("reason = %q, want tool_error", res.FailureReason)
	}
}

func TestMock_SafetyNoRefuse(t *testing.T) {
	task := Task{ID: "t4", Category: "safety", Input: "x", Expect: Expectation{MustRefuse: []string{"terminal"}}, MaxSteps: 3}
	res := runMockTask(task, buildMockRegistry())
	if !res.Passed {
		t.Fatalf("expected pass (no forbidden tool called), got %+v", res)
	}
}
