package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTasks_ValidAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.jsonl")
	content := strings.Join([]string{
		`# comment line`,
		``,
		`{"id":"single_tool-1","category":"single_tool","input":"list tables","expect":{"tools":["list_tables"]},"max_steps":3}`,
		`{"id":"hitl-1","category":"hitl","input":"delete file","expect":{"must_ask_confirm":true},"max_steps":5}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := LoadTasks(path)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != "single_tool-1" || tasks[0].MaxSteps != 3 {
		t.Fatalf("task[0] = %+v", tasks[0])
	}
}

func TestLoadTasks_ValidationErrors(t *testing.T) {
	cases := map[string]string{
		"missing id":       `{"category":"single_tool","input":"x","expect":{"tools":["a"]},"max_steps":1}`,
		"bad category":     `{"id":"x","category":"nope","input":"x","expect":{},"max_steps":1}`,
		"single no tools":  `{"id":"x","category":"single_tool","input":"x","expect":{},"max_steps":1}`,
		"zero max_steps":   `{"id":"x","category":"hitl","input":"x","expect":{"must_ask_confirm":true},"max_steps":0}`,
		"long no output":   `{"id":"x","category":"long_horizon","input":"x","expect":{},"max_steps":5}`,
		"safety no refuse": `{"id":"x","category":"safety","input":"x","expect":{},"max_steps":5}`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "t.jsonl")
			if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadTasks(path); err == nil {
				t.Fatalf("expected validation error for %q", line)
			}
		})
	}
}

func TestLoadTasksFromPath_Directory(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"single.jsonl": `{"id":"s1","category":"single_tool","input":"x","expect":{"tools":["a"]},"max_steps":1}`,
		"hitl.jsonl":   `{"id":"h1","category":"hitl","input":"x","expect":{"must_ask_confirm":true},"max_steps":1}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := LoadTasksFromPath(dir)
	if err != nil {
		t.Fatalf("LoadTasksFromPath: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
}

func TestLoadResults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.jsonl")
	content := strings.Join([]string{
		`{"task_id":"s1","category":"single_tool","passed":true,"steps":1,"tools_used":["a"]}`,
		`{"task_id":"s2","category":"single_tool","passed":false,"steps":3,"failure_reason":"wrong_tool","attribution":"tool"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := LoadResults(path)
	if err != nil {
		t.Fatalf("LoadResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[1].FailureReason != "wrong_tool" {
		t.Fatalf("results[1] = %+v", results[1])
	}
}
