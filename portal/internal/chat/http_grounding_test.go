package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func TestHTTPRequestGrounded_ComposesSkillPortWithSQLHost(t *testing.T) {
	skill := `POST http://{mgr_ipv4_address}:49997/v1/taskmanager/exec?shell=ps`
	sql := `{"mgr_ipv4_address":"10.131.23.43","vmid":9076}`
	a := newHTTPAnchorSet()
	ingestHTTPText(&a, skill)
	ingestHTTPText(&a, sql)
	ok := "http://10.131.23.43:49997/v1/taskmanager/exec?shell=ps"
	if !httpRequestGrounded(ok, a) {
		t.Fatalf("skill+sql must ground xagent exec")
	}
	for _, bad := range []string{
		"http://10.131.23.43:10021/api/v1/log/search",
		"http://10.131.23.43:8080/health",
		"http://10.131.23.43:49997/api/v1/log/search",
	} {
		if httpRequestGrounded(bad, a) {
			t.Errorf("invented URL must not be grounded: %s", bad)
		}
	}
}

func TestHTTPRequestGrounded_EstablishedOriginAllowsNewPath(t *testing.T) {
	a := newHTTPAnchorSet()
	ingestHTTPText(&a, `GET http://es.internal:9200/`)
	if !httpRequestGrounded("http://es.internal:9200/app-*/_search", a) {
		t.Fatal("same origin, new ES path must pass")
	}
}

func TestHTTPRequestGrounded_MarkdownBacktickESURL(t *testing.T) {
	skill := "已知为 `http://10.137.212.70:29200`\n只能 POST http://{ip}:49997/v1/taskmanager/exec?shell=ps"
	a := newHTTPAnchorSet()
	ingestHTTPText(&a, skill)
	es := "http://10.137.212.70:29200/backend-union-access-*/_search"
	if !httpRequestGrounded(es, a) {
		t.Fatalf("backtick-wrapped esUrl must be an anchor; ports=%v hostPorts=%v", a.ports, a.hostPorts)
	}
}

func TestTurnIntentGate_UngroundedHTTPRetryOnceThenFinish(t *testing.T) {
	gate := TurnIntentGate{ToolFamily: map[string]string{"http_request": FamilyCore}}
	req := &agent.Request{
		SystemPrompt: `POST http://{ip}:49997/v1/taskmanager/exec?shell=ps`,
		Messages: []model.Message{
			{Role: "user", Content: "查日志"},
			{Role: "tool", Content: `{"mgr_ipv4_address":"10.131.23.43"}`},
		},
	}
	step := model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
		{ID: "1", Name: "http_request", Arguments: map[string]any{
			"method": "POST", "url": "http://10.131.23.43:10021/api/v1/log/search",
		}},
	}}
	trace := &agent.RunTrace{}
	in := agent.PostModelPolicyInput{Req: req, AssistantText: "搜 ES", ToolStep: step, Trace: trace}
	first := gate.Evaluate(context.Background(), in)
	if first.Decision != agent.PostModelRetry || first.Reason != "http_ungrounded" {
		t.Fatalf("first %v %q", first.Decision, first.Reason)
	}
	if trace.HTTPUngroundedNudges != 1 {
		t.Fatalf("nudges=%d", trace.HTTPUngroundedNudges)
	}
	second := gate.Evaluate(context.Background(), in)
	if second.Decision != agent.PostModelFinish || second.Reason != "http_ungrounded" {
		t.Fatalf("second %v %q", second.Decision, second.Reason)
	}
}

func TestHTTPRequestGrounded_InactiveAllowsFirstHop(t *testing.T) {
	a := newHTTPAnchorSet()
	if !httpRequestGrounded("http://example.com/anything", a) {
		t.Fatal("no anchors → do not block")
	}
}

func TestHTTPRequestGrounded_StickinessSameHostPort(t *testing.T) {
	a := newHTTPAnchorSet()
	ingestHTTPText(&a, `POST http://10.131.23.43:49997/v1/taskmanager/exec?shell=ps`)
	if !httpRequestGrounded("http://10.131.23.43:49997/v1/taskmanager/exec?shell=ps", a) {
		t.Fatal("repeat same URL")
	}
	if httpRequestGrounded("http://10.131.23.43:10021/api/v1/log/search", a) {
		t.Fatal("new port on known host")
	}
}

func TestFilterUnofferedTools_DropsHallucinatedMCP(t *testing.T) {
	calls := []model.ToolCall{
		{ID: "1", Name: "http_request"},
		{ID: "2", Name: "pods_list"},
	}
	kept, n := filterUnofferedTools(calls, map[string]string{"http_request": FamilyCore})
	if n != 1 || len(kept) != 1 || kept[0].Name != "http_request" {
		t.Fatalf("kept=%#v dropped=%d", kept, n)
	}
}

func TestTurnIntentGate_UngroundedHTTPRetry(t *testing.T) {
	gate := TurnIntentGate{ToolFamily: map[string]string{"http_request": FamilyCore}}
	req := &agent.Request{
		SystemPrompt: `POST http://{ip}:49997/v1/taskmanager/exec?shell=ps`,
		Messages: []model.Message{
			{Role: "user", Content: `vm里的日志有可能在D:\module\xstream\apps\cgvmagent\current\目录下`},
			{Role: "tool", Content: `{"mgr_ipv4_address":"10.131.23.43"}`},
		},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           req,
		AssistantText: "去查日志",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "http_request", Arguments: map[string]any{
				"method": "POST", "url": "http://10.131.23.43:10021/api/v1/log/search",
			}},
			{ID: "2", Name: "pods_list", Arguments: map[string]any{"labelSelector": "vm-id=9076"}},
		}},
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "http_ungrounded" {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_KeepsGroundedHTTPDropsInvented(t *testing.T) {
	gate := TurnIntentGate{ToolFamily: map[string]string{"http_request": FamilyCore}}
	req := &agent.Request{
		SystemPrompt: `POST http://{ip}:49997/v1/taskmanager/exec?shell=ps`,
		Messages: []model.Message{
			{Role: "user", Content: "卸载存档失败"},
			{Role: "tool", Content: `{"mgr_ipv4_address":"10.131.23.43"}`},
		},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           req,
		AssistantText: "进实例",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "http_request", Arguments: map[string]any{
				"method": "POST",
				"url":    "http://10.131.23.43:49997/v1/taskmanager/exec?shell=ps",
			}},
			{ID: "2", Name: "pods_list", Arguments: map[string]any{"labelSelector": "vm-id=9076"}},
			{ID: "3", Name: "http_request", Arguments: map[string]any{
				"method": "GET", "url": "http://10.131.23.43:8080/health",
			}},
		}},
	})
	if res.Decision != agent.PostModelFilter {
		t.Fatalf("got %v %q calls=%#v", res.Decision, res.Reason, res.ToolCalls)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "1" {
		t.Fatalf("keep only grounded exec, got %#v reason=%s", res.ToolCalls, res.Reason)
	}
}

func TestPrepareTurnToolSurface_GenericVMLogDoesNotActivateK8s(t *testing.T) {
	t.Setenv(turnToolSurfaceEnv, "1")
	servers := []*biz.McpServerMeta{
		{ID: "k8s-dev", Name: "Kubernetes"},
		{ID: "gitlab", Name: "GitLab"},
	}
	active, _ := PrepareTurnToolSurface(
		context.Background(),
		`vm里的日志有可能在D:\module\xstream\apps\cgvmagent\current\目录下`,
		nil, servers, nil, nil,
	)
	if FamilyActive(active, "mcp:k8s-dev") {
		t.Fatalf("k8s must not activate without k8s vocabulary: %v", active)
	}
}
