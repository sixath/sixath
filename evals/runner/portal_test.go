package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/framework/model"
)

type fakePortal struct {
	createStatus int
	streamBody   string
	answer       string
	slowStream   time.Duration
	// persistAfterStream 为 true 时 assistant 消息在 stream handler 写完正文并等待 persistDelay 后才可见（模拟服务端先写 error 帧再落库）。
	persistAfterStream bool
	persistDelay       time.Duration
	// getFailures 前 N 次 GET messages 返回 500。
	getFailures int32
	getDelay    time.Duration

	mu        sync.Mutex
	gotAuth   string
	gotOrg    string
	gotBody   string
	persisted bool
	creates   int32
	gets      int32
}

func (f *fakePortal) auth() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotAuth, f.gotOrg
}

func (f *fakePortal) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agents/{agent}/sessions", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.creates, 1)
		f.mu.Lock()
		f.gotAuth, f.gotOrg = r.Header.Get("Authorization"), r.Header.Get("X-Org-Id")
		f.mu.Unlock()
		if f.createStatus != 0 {
			w.WriteHeader(f.createStatus)
			_, _ = w.Write([]byte(`{"ret":{"code":500,"message":"boom"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ret":{"code":0},"id":"sess-1","agentId":"` + r.PathValue("agent") + `"}`))
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages/stream", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		f.mu.Lock()
		f.gotBody = string(buf[:n])
		f.mu.Unlock()
		if f.slowStream > 0 {
			time.Sleep(f.slowStream)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(f.streamBody))
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		if f.persistDelay > 0 {
			time.Sleep(f.persistDelay)
		}
		f.mu.Lock()
		f.persisted = true
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /api/v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&f.gets, 1) <= atomic.LoadInt32(&f.getFailures) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if f.getDelay > 0 {
			time.Sleep(f.getDelay)
		}
		f.mu.Lock()
		visible := f.answer != "" && (!f.persistAfterStream || f.persisted)
		f.mu.Unlock()
		if !visible {
			_, _ = w.Write([]byte(`{"ret":{"code":0},"items":[{"role":"user","content":"q"}]}`))
			return
		}
		fmt.Fprintf(w, `{"ret":{"code":0},"items":[{"role":"user","content":"q"},{"role":"assistant","content":%q,"metadata":{"timeline":[{"kind":"tool","toolName":"es_log_query","phase":"completed","result":{"total":2}}]}}]}`, f.answer)
	})
	return mux
}

const sseDone = "event: chunk\ndata: {\"content\":\"草稿\"}\n\nevent: done\ndata: {\"content\":\"\",\"done\":true}\n\n"

func newTestClient(url string) *PortalClient {
	return &PortalClient{BaseURL: url, Token: "tok", OrgID: "default", HTTP: http.DefaultClient,
		Timeout: 2 * time.Second, PollInterval: 10 * time.Millisecond, GracePeriod: time.Second}
}

func TestPortalClient_RunTurn(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002\n198065"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "agent-1", "eval-x", "q")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Failed || turn.HITL || turn.TimedOut || turn.Answer != "198002\n198065" || len(turn.Timeline) != 1 {
		t.Fatalf("turn=%+v", turn)
	}
	if auth, org := f.auth(); auth != "Bearer tok" || org != "default" {
		t.Fatalf("auth header=%q org=%q", auth, org)
	}
	f.mu.Lock()
	body := f.gotBody
	f.mu.Unlock()
	if body != `{"content":"q"}` {
		t.Fatalf("stream body=%q", body)
	}
}

func TestPortalClient_SSEErrorIsRunFailure(t *testing.T) {
	f := &fakePortal{streamBody: "event: error\ndata: {\"error\":\"model timeout\"}\n\n"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.Failed || !strings.Contains(turn.Error, "model timeout") {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_SSEErrorWaitsForPersistedTimeline(t *testing.T) {
	f := &fakePortal{
		streamBody:         "event: error\ndata: {\"error\":\"react agent reached max steps: 30\"}\n\n",
		answer:             "Error: react agent reached max steps: 30",
		persistAfterStream: true,
		persistDelay:       100 * time.Millisecond,
	}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.Failed || len(turn.Timeline) != 1 {
		t.Fatalf("timeline persisted after the error frame must be fetched: turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_SSEErrorTimelineSurvivesTurnTimeout(t *testing.T) {
	f := &fakePortal{
		streamBody:         "event: error\ndata: {\"error\":\"react agent reached max steps: 30\"}\n\n",
		answer:             "Error: react agent reached max steps: 30",
		persistAfterStream: true,
		persistDelay:       100 * time.Millisecond,
		getDelay:           150 * time.Millisecond,
	}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	c := newTestClient(ts.URL)
	c.Timeout = 180 * time.Millisecond
	turn, err := c.RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.Failed || len(turn.Timeline) != 1 {
		t.Fatalf("timeline fetch must not use the expired turn ctx: turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_StreamCutFallsBackToPolling(t *testing.T) {
	f := &fakePortal{streamBody: "event: chunk\ndata: {\"content\":\"半截\"}\n\n", answer: "最终答案"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || turn.Answer != "最终答案" || turn.Failed {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_PolledErrorMessageIsFailure(t *testing.T) {
	f := &fakePortal{streamBody: "event: chunk\ndata: {\"content\":\"半截\"}\n\n", answer: "Error: chat completion: 502 Bad Gateway"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.Failed || turn.Error != "chat completion: 502 Bad Gateway" || len(turn.Timeline) != 1 || turn.Answer != "" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_HITLEventFlagsTurn(t *testing.T) {
	body := "event: confirm_required\ndata: {\"confirmation\":{\"token\":\"x\"}}\n\n" + sseDone
	f := &fakePortal{streamBody: body, answer: "请确认"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.HITL || turn.Answer != "请确认" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_TransientPollErrors(t *testing.T) {
	cut := "event: chunk\ndata: {\"content\":\"半截\"}\n\n"
	f := &fakePortal{streamBody: cut, answer: "ok", getFailures: 3}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || turn.Answer != "ok" {
		t.Fatalf("3 transient poll errors must be tolerated: turn=%+v err=%v", turn, err)
	}

	f2 := &fakePortal{streamBody: cut, answer: "ok", getFailures: 4}
	ts2 := httptest.NewServer(f2.handler(t))
	defer ts2.Close()
	if _, err := newTestClient(ts2.URL).RunTurn(context.Background(), "a", "t", "q"); err == nil {
		t.Fatal("4 consecutive poll errors must give up")
	}
}

func TestPortalClient_LargeFrameDoesNotBreakStream(t *testing.T) {
	big := strings.Repeat("x", 5<<20)
	body := "event: tool_call\ndata: {\"tool_call\":{\"result\":\"" + big + "\"}}\n\n" + sseDone
	f := &fakePortal{streamBody: body, answer: "ok"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || turn.Failed || turn.Answer != "ok" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_DoneWithoutPersistedReplyIsError(t *testing.T) {
	f := &fakePortal{streamBody: sseDone}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	if _, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q"); err == nil {
		t.Fatal("done without assistant message must be an error")
	}
}

func TestPortalClient_InfraErrors(t *testing.T) {
	f := &fakePortal{createStatus: http.StatusInternalServerError}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	_, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err == nil {
		t.Fatal("5xx on create session must be an error")
	}
	if strings.Contains(err.Error(), "tok") {
		t.Fatalf("error leaks token: %v", err)
	}
}

func TestPortalClient_TimeoutWaitsForLeftoverRun(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, slowStream: 300 * time.Millisecond, answer: "late", persistAfterStream: true}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	c := newTestClient(ts.URL)
	c.Timeout = 50 * time.Millisecond
	c.GracePeriod = 2 * time.Second
	start := time.Now()
	turn, err := c.RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.TimedOut || len(turn.Timeline) != 1 {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if el := time.Since(start); el < 250*time.Millisecond {
		t.Fatalf("returned after %s, before the leftover server run persisted", el)
	}
}

func TestPortalClient_TimeoutGraceExpires(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, slowStream: 800 * time.Millisecond, answer: "late", persistAfterStream: true}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	c := newTestClient(ts.URL)
	c.Timeout = 50 * time.Millisecond
	c.GracePeriod = 100 * time.Millisecond
	start := time.Now()
	turn, err := c.RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.TimedOut || len(turn.Timeline) != 0 {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if el := time.Since(start); el > 600*time.Millisecond {
		t.Fatalf("grace period not honored: %s", el)
	}
}

func TestClassifyPortalTurn(t *testing.T) {
	tl := []any{map[string]any{"kind": "tool", "toolName": "es_log_query", "phase": "completed", "result": map[string]any{"total": 2.0}}}
	cases := []struct {
		name       string
		turn       portalTurn
		err        error
		wantReason string
		wantAttr   string
		wantJudge  bool
		wantMax    bool
		wantTrace  bool
	}{
		{"transport/http error", portalTurn{}, errors.New("portal: create session: HTTP 503"), "infra_error", "", false, false, false},
		{"answer goes to judge", portalTurn{Answer: "x", Timeline: tl}, nil, "", "", true, false, false},
		{"client timeout", portalTurn{TimedOut: true, Error: "turn timeout", Timeline: tl}, nil, "timeout", "harness", false, false, true},
		{"max steps", portalTurn{Failed: true, Error: "react agent reached max steps: 502", Timeline: tl}, nil, "model_error", "model", false, true, true},
		{"bad gateway", portalTurn{Failed: true, Error: "chat completion: Bad Gateway"}, nil, "infra_error", "", false, false, false},
		{"vmid digits are not a status", portalTurn{Failed: true, Error: "react failed: vmid 15029 not found", Timeline: tl}, nil, "run_error", "harness", false, false, true},
		{"bare number is not a status", portalTurn{Failed: true, Error: "upstream returned 503", Timeline: tl}, nil, "run_error", "harness", false, false, true},
		{"status code 502", portalTurn{Failed: true, Error: "upstream status code: 502"}, nil, "infra_error", "", false, false, false},
		{"HTTP 503", portalTurn{Failed: true, Error: "HTTP 503"}, nil, "infra_error", "", false, false, false},
		{"status 504", portalTurn{Failed: true, Error: "status 504"}, nil, "infra_error", "", false, false, false},
		{"status=504", portalTurn{Failed: true, Error: "request failed: status=504"}, nil, "infra_error", "", false, false, false},
		{"504 gateway timeout", portalTurn{Failed: true, Error: "504 Gateway Timeout"}, nil, "infra_error", "", false, false, false},
		{"429 too many requests", portalTurn{Failed: true, Error: "429 Too Many Requests"}, nil, "infra_error", "", false, false, false},
		{"max steps 502", portalTurn{Failed: true, Error: "max steps: 502", Timeline: tl}, nil, "model_error", "model", false, true, true},
		{"service unavailable", portalTurn{Failed: true, Error: "Service Unavailable"}, nil, "infra_error", "", false, false, false},
		{"connection refused", portalTurn{Failed: true, Error: "dial tcp: connection refused"}, nil, "infra_error", "", false, false, false},
		{"connection reset", portalTurn{Failed: true, Error: "read: connection reset by peer"}, nil, "infra_error", "", false, false, false},
		{"caller identity", portalTurn{Failed: true, Error: "caller identity is required"}, nil, "infra_error", "", false, false, false},
		{"inbound disabled", portalTurn{Failed: true, Error: "public chat inbound is disabled"}, nil, "infra_error", "", false, false, false},
		{"401 unauthorized", portalTurn{Failed: true, Error: "401 Unauthorized"}, nil, "infra_error", "", false, false, false},
		{"HTTP 403", portalTurn{Failed: true, Error: "HTTP 403"}, nil, "infra_error", "", false, false, false},
		{"status code 401", portalTurn{Failed: true, Error: "status code: 401"}, nil, "infra_error", "", false, false, false},
		{"403 forbidden", portalTurn{Failed: true, Error: "403 Forbidden"}, nil, "infra_error", "", false, false, false},
		{"k8s forbidden is a run error", portalTurn{Failed: true, Error: "pods is forbidden: User cannot list", Timeline: tl}, nil, "run_error", "harness", false, false, true},
		{"bare unauthorized is a run error", portalTurn{Failed: true, Error: "tool: unauthorized to read secret", Timeline: tl}, nil, "run_error", "harness", false, false, true},
		{"rate limit", portalTurn{Failed: true, Error: "Rate limit exceeded"}, nil, "infra_error", "", false, false, false},
		{"429", portalTurn{Failed: true, Error: "HTTP 429"}, nil, "infra_error", "", false, false, false},
		{"too many requests", portalTurn{Failed: true, Error: "Too Many Requests"}, nil, "infra_error", "", false, false, false},
		{"other run error", portalTurn{Failed: true, Error: "context deadline exceeded", Timeline: tl}, nil, "run_error", "harness", false, false, true},
		{"hitl", portalTurn{HITL: true, Answer: "请确认", Timeline: tl}, nil, "hitl_required", "harness", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, judge := classifyPortalTurn(shapeTask, tc.turn, tc.err)
			if judge != tc.wantJudge {
				t.Fatalf("judge=%v", judge)
			}
			if judge {
				return
			}
			if r.FailureReason != tc.wantReason || r.Attribution != tc.wantAttr || r.HitMaxSteps != tc.wantMax || r.Passed || r.TaskID != shapeTask.ID {
				t.Fatalf("r=%+v", r)
			}
			if tc.wantTrace && (r.Trace == nil || len(r.Trace.Calls) != 1 || len(r.ToolsUsed) != 1 || r.Steps != 2) {
				t.Fatalf("counted failure must keep trace: %+v", r)
			}
			if r.Error == "" {
				t.Fatal("failure must carry an error message")
			}
		})
	}
}

func TestRunPortal_ScoresAndMerges(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass, allPass}}}
	res := runPortal(context.Background(), []Task{shapeTask}, newTestClient(ts.URL), portalRunOptions{AgentID: "a", RunID: "r1", Judge: j, Repeat: 2, Concurrency: 1})
	if len(res) != 1 || res[0].Runs != 2 || !res[0].Passed || res[0].Trace == nil || len(res[0].Trace.Calls) != 1 {
		t.Fatalf("res=%+v", res)
	}
}

func TestRunPortalTask_SSEErrorEndToEnd(t *testing.T) {
	cases := []struct {
		errMsg     string
		wantReason string
		wantMax    bool
	}{
		{"react agent reached max steps: 30", "model_error", true},
		{"chat completion: 502 Bad Gateway", "infra_error", false},
		{"tool registry exploded", "run_error", false},
	}
	for _, tc := range cases {
		t.Run(tc.errMsg, func(t *testing.T) {
			f := &fakePortal{streamBody: fmt.Sprintf("event: error\ndata: {\"error\":%q}\n\n", tc.errMsg), answer: "Error: " + tc.errMsg}
			ts := httptest.NewServer(f.handler(t))
			defer ts.Close()
			r := runPortalTask(context.Background(), newTestClient(ts.URL), portalRunOptions{AgentID: "a", RunID: "r"}, shapeTask, 0)
			if r.FailureReason != tc.wantReason || r.HitMaxSteps != tc.wantMax || r.Passed || !strings.Contains(r.Error, tc.errMsg) {
				t.Fatalf("r=%+v", r)
			}
		})
	}
}

// blockingJudgeModel 阻塞到 ctx 结束，用来验证 judge 超时。
type blockingJudgeModel struct{ stubJudgeModel }

func (m *blockingJudgeModel) Chat(ctx context.Context, _ []model.Message, _ ...model.Option) (*model.Generation, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunPortalTask_JudgeTimeout(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	start := time.Now()
	r := runPortalTask(context.Background(), newTestClient(ts.URL),
		portalRunOptions{AgentID: "a", RunID: "r", Judge: &Judge{Model: &blockingJudgeModel{}}, JudgeTimeout: 50 * time.Millisecond}, shapeTask, 0)
	if r.FailureReason != "judge_error" || time.Since(start) > time.Second {
		t.Fatalf("r=%+v elapsed=%s", r, time.Since(start))
	}
}

// lockedJudgeModel 并发安全的 judge 桩，总是返回全部通过。
type lockedJudgeModel struct {
	mu    sync.Mutex
	calls int
}

func (m *lockedJudgeModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return nil, errors.New("unused")
}

func (m *lockedJudgeModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return &model.Generation{Text: allPass}, nil
}

func (m *lockedJudgeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestRunPortal_ConcurrentWithJudge(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	var tasks []Task
	for i := 0; i < 5; i++ {
		tk := shapeTask
		tk.ID = fmt.Sprintf("s%d", i)
		tasks = append(tasks, tk)
	}
	tasks = append(tasks, Task{ID: "o", Category: "tool_selection", Input: "x"})
	jm := &lockedJudgeModel{}
	res := runPortal(context.Background(), tasks, newTestClient(ts.URL), portalRunOptions{AgentID: "a", RunID: "r", Judge: &Judge{Model: jm}, Repeat: 2, Concurrency: 3})
	if len(res) != 5 {
		t.Fatalf("only answer_shape tasks run: %+v", res)
	}
	for i, r := range res {
		if r.TaskID != fmt.Sprintf("s%d", i) || !r.Passed || r.Runs != 2 {
			t.Fatalf("res[%d]=%+v", i, r)
		}
	}
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.calls != 10 {
		t.Fatalf("judge calls=%d", jm.calls)
	}
}

func TestRunPortal_CanceledContextStartsNothing(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t2 := shapeTask
	t2.ID = "s2"
	res := runPortal(ctx, []Task{shapeTask, t2}, newTestClient(ts.URL), portalRunOptions{AgentID: "a", RunID: "r", Concurrency: 1})
	if len(res) != 2 || atomic.LoadInt32(&f.creates) != 0 {
		t.Fatalf("res=%+v creates=%d", res, f.creates)
	}
	for _, r := range res {
		if r.FailureReason != "infra_error" || r.InfraErrors != 1 || r.Runs != 0 {
			t.Fatalf("skipped task must be a lost run: %+v", r)
		}
	}
}
