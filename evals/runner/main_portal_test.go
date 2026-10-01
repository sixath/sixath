package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePortalTasks(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const portalShapeTask = `{"id":"s","category":"answer_shape","input":"q","max_steps":30,"expect":{"answer_type":"count"}}` + "\n"

func TestRunPortalMode_RequiresJudgeAndAgent(t *testing.T) {
	tasks := writePortalTasks(t, t.TempDir(), portalShapeTask)

	err := runPortalMode(portalModeConfig{TasksPath: tasks, PortalURL: "http://x", AgentID: ""})
	if err == nil || !strings.Contains(err.Error(), "-agent") {
		t.Fatalf("err=%v", err)
	}
	err = runPortalMode(portalModeConfig{TasksPath: tasks, PortalURL: "", AgentID: "a"})
	if err == nil || !strings.Contains(err.Error(), "-portal-url") {
		t.Fatalf("err=%v", err)
	}
	err = runPortalMode(portalModeConfig{TasksPath: tasks, PortalURL: "http://x", AgentID: "a"})
	if err == nil || !strings.Contains(err.Error(), "-judge-model") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunPortalMode_RequiresAnswerShapeTasks(t *testing.T) {
	tasks := writePortalTasks(t, t.TempDir(), `{"id":"x","category":"single_tool","input":"q","max_steps":5,"expect":{"tools":["list_tables"]}}`+"\n")
	err := runPortalMode(portalModeConfig{
		TasksPath: tasks, PortalURL: "http://x", AgentID: "a",
		Judge: &Judge{Model: &stubJudgeModel{replies: []string{allPass}}},
	})
	if err == nil || !strings.Contains(err.Error(), "answer_shape") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunReport_AnswerShapeGateAndBaselineGuard(t *testing.T) {
	dir := t.TempDir()
	tasks := writePortalTasks(t, dir, portalShapeTask)
	resultsPath := filepath.Join(dir, "r.jsonl")
	lost := []TaskResult{{TaskID: "s", Category: "answer_shape", FailureReason: "infra_error", Runs: 1, InfraErrors: 1}}
	if err := writeResults(resultsPath, lost); err != nil {
		t.Fatal(err)
	}
	baselineOut := filepath.Join(dir, "baseline.json")

	err := runReport(resultsPath, tasks, "", "", filepath.Join(dir, "report.json"), baselineOut, "report")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("report mode must apply the answer_shape gate, err=%v", err)
	}
	if _, statErr := os.Stat(baselineOut); !os.IsNotExist(statErr) {
		t.Fatalf("baseline must not be written when the gate fails, stat err=%v", statErr)
	}
}

func TestRunPortalMode_EndToEnd(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "3 台"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	dir := t.TempDir()
	tasks := writePortalTasks(t, dir, portalShapeTask+`{"id":"x","category":"single_tool","input":"q","max_steps":5,"expect":{"tools":["list_tables"]}}`+"\n")
	resultsOut := filepath.Join(dir, "r.jsonl")

	err := runPortalMode(portalModeConfig{
		TasksPath: tasks, PortalURL: ts.URL + "/", AgentID: "a", Token: "tok", Repeat: 1, Concurrency: 1,
		Judge:  &Judge{Model: &stubJudgeModel{replies: []string{allPass}}},
		Finish: finishOpts{ResultsOut: resultsOut, Out: filepath.Join(dir, "report.json")},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := LoadResults(resultsOut)
	if err != nil || len(res) != 1 || res[0].TaskID != "s" || !res[0].Passed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
