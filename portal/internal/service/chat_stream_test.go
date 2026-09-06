package service

import (
	"testing"

	agent "github.com/sixath/framework/harness"
	toolskill "github.com/sixath/framework/tool/skillops"
)

func TestConfirmationRequestsFromResponseExtractsPendingSkillManage(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm skill create.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_sm",
					ToolName:   "skill_manage",
					Result: map[string]any{
						"status":     "pending",
						"token":      "sm_tok",
						"action":     "create",
						"name":       "my-skill",
						"preview":    "---\nname: my-skill\n---\n# body",
						"expires_in": 300,
					},
				}},
			},
		},
	}
	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "skill_manage" || got.Token != "sm_tok" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.DSL != "---\nname: my-skill\n---\n# body" {
		t.Fatalf("unexpected preview dsl: %q", got.DSL)
	}
	if got.Title != "Confirm skill create" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.ResourceKey != "create:my-skill" {
		t.Fatalf("resource_key=%q", got.ResourceKey)
	}
}

func TestConfirmationRequestsFromResponseExtractsPendingSkillManageStruct(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm skill patch.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_sm_patch",
					ToolName:   "skill_manage",
					Result: toolskill.SkillManagePendingResponse{
						Status:    "pending",
						Token:     "patch_tok",
						Action:    "patch",
						Name:      "archive-move-ops",
						Preview:   "Patch archive-move-ops:\n- old\n+ new",
						ExpiresIn: 300,
					},
				}},
			},
		},
	}
	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "skill_manage" || got.Token != "patch_tok" || got.Title != "Confirm skill patch" {
		t.Fatalf("unexpected: %#v", got)
	}
	if got.ResourceKey != "patch:archive-move-ops" {
		t.Fatalf("resource_key=%q, want patch:archive-move-ops", got.ResourceKey)
	}
}

func TestConfirmationRequestsFromResponse_SkipsSkillManageAlreadyConfirmedInSameTurn(t *testing.T) {
	resp := &agent.Response{
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{
					{
						ToolCallID: "call_propose",
						ToolName:   "skill_manage",
						Arguments: map[string]any{
							"action": "patch", "name": "scheduling-flow-trace",
							"old_string": "a", "new_string": "b",
						},
						Result: map[string]any{
							"status": "pending",
							"token":  "tok-alive",
							"action": "patch",
							"name":   "scheduling-flow-trace",
							"preview": "Patch ...",
						},
					},
					{
						ToolCallID: "call_self_confirm",
						ToolName:   "skill_manage",
						Arguments: map[string]any{
							"action": "patch", "name": "scheduling-flow-trace",
							"confirm_token": "tok-alive",
						},
						Result: map[string]any{"status": "ok", "name": "scheduling-flow-trace"},
					},
				},
			},
		},
	}
	items := confirmationRequestsFromResponse(resp)
	if len(items) != 0 {
		t.Fatalf("expected no confirm card after agent self-confirm, got %#v", items)
	}
}

func TestConfirmResultFromSkillManageMap_Success(t *testing.T) {
	got := confirmResultFromSkillManageMap("tok1", map[string]any{
		"status": "ok",
		"action": "patch",
		"name":   "archive-move-ops",
	})
	if got == nil || !got.OK || got.Kind != "skill_manage" || got.Token != "tok1" {
		t.Fatalf("unexpected success payload: %#v", got)
	}
	if got.Error != "" || got.ErrorCode != "" {
		t.Fatalf("unexpected error fields: %#v", got)
	}
}

func TestConfirmResultFromSkillManageMap_Error(t *testing.T) {
	got := confirmResultFromSkillManageMap("old_tok", map[string]any{
		"error":      "确认已失效：已被更新的提案替换，请确认最新卡片",
		"error_code": "superseded",
	})
	if got == nil || got.OK || got.Kind != "skill_manage" || got.Token != "old_tok" {
		t.Fatalf("unexpected error payload: %#v", got)
	}
	if got.ErrorCode != "superseded" {
		t.Fatalf("error_code=%q", got.ErrorCode)
	}
	if got.Error == "" {
		t.Fatalf("expected error message")
	}
}

func TestConfirmationRequestsFromResponseExtractsPendingExecuteWrite(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm before execution.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolName: "execute_write",
					Result: map[string]any{
						"status":     "pending",
						"token":      "abc123",
						"dsl":        "UPDATE users SET active = 0 WHERE id = 7",
						"expires_in": 300,
					},
				}},
			},
		},
	}

	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "execute_write" || got.Token != "abc123" {
		t.Fatalf("unexpected confirmation identity: %#v", got)
	}
	if got.DSL != "UPDATE users SET active = 0 WHERE id = 7" {
		t.Fatalf("unexpected dsl: %q", got.DSL)
	}
	if got.ExpiresIn != 300 || got.Severity != "danger" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}

func TestConfirmationRequestsFromResponseExtractsPendingTerminal(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm shell.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_term",
					ToolName:   "terminal",
					Result: map[string]any{
						"status":     "pending",
						"token":      "term_tok",
						"command":    "rm -rf ./build",
						"expires_in": 300,
					},
				}},
			},
		},
	}
	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "terminal" || got.Token != "term_tok" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.DSL != "rm -rf ./build" || got.Severity != "danger" {
		t.Fatalf("unexpected payload: %#v", got)
	}
	if got.Title != "Confirm terminal command" {
		t.Fatalf("title=%q", got.Title)
	}
}

func TestConfirmationRequestsFromResponseExtractsPendingWorkspaceFile(t *testing.T) {
	resp := &agent.Response{
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_wf",
					ToolName:   "write_file",
					Result: map[string]any{
						"status":     "pending",
						"token":      "wf_tok",
						"path":       ".env",
						"preview":    "write .env (8 bytes)",
						"expires_in": 300,
					},
				}},
			},
		},
	}
	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "workspace_file" || got.Token != "wf_tok" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.DSL != "write .env (8 bytes)" || got.Severity != "danger" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestConfirmationRequestsFromResponseExtractsPendingBrowser(t *testing.T) {
	resp := &agent.Response{
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_nav",
					ToolName:   "browser_navigate",
					Result: map[string]any{
						"ok":         true,
						"status":     "pending",
						"token":      "br_tok",
						"action":     "navigate",
						"preview":    "navigate https://example.com/",
						"expires_in": 300,
					},
				}},
			},
		},
	}
	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "browser" || got.Token != "br_tok" {
		t.Fatalf("%#v", got)
	}
	if got.DSL != "navigate https://example.com/" {
		t.Fatalf("dsl=%q", got.DSL)
	}
}

func TestConfirmationRequestsFromResponseIgnoresMalformedResults(t *testing.T) {
	resp := &agent.Response{
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolName: "execute_write",
					Result: map[string]any{
						"status": "pending",
						"dsl":    "DELETE FROM users",
					},
				}},
			},
		},
	}

	if got := confirmationRequestsFromResponse(resp); len(got) != 0 {
		t.Fatalf("expected malformed confirmation to be ignored, got %#v", got)
	}
}

func TestInputRequestsFromResponseExtractsPendingAskUser(t *testing.T) {
	resp := &agent.Response{
		Text: "Need your password.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_1",
					ToolName:   "ask_user",
					Result: map[string]any{
						"status":     "pending",
						"request_id": "req_abc",
						"token":      "tok_xyz",
						"kind":       "password",
						"field":      "ssh_password",
						"prompt":     "Enter SSH password",
						"title":      "SSH Password",
						"expires_in": 600,
					},
				}},
			},
		},
	}
	items := inputRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("got %d", len(items))
	}
	got := items[0]
	if got.Token != "tok_xyz" || got.Kind != "password" || got.ID != "call_1:tok_xyz" {
		t.Fatalf("%#v", got)
	}
	if got.Severity != "warning" {
		t.Fatalf("severity=%q", got.Severity)
	}
}

func TestStreamEventsFromResponse_InputBeforeConfirm(t *testing.T) {
	resp := &agent.Response{
		Text: "Need input.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{
					{ToolCallID: "c1", ToolName: "ask_user", Result: map[string]any{
						"status": "pending", "request_id": "r1", "token": "t1",
						"kind": "text", "field": "username", "prompt": "Username?",
					}},
					{ToolCallID: "c2", ToolName: "execute_write", Result: map[string]any{
						"status": "pending", "token": "wt", "dsl": "DELETE FROM x",
					}},
				},
			},
		},
	}
	events := streamEventsFromResponse(resp)
	if len(events) != 3 {
		t.Fatalf("want chunk+input+confirm, got %d: %#v", len(events), events)
	}
	if events[1].Type != ChatStreamEventInputRequired || events[2].Type != ChatStreamEventConfirmRequired {
		t.Fatalf("order wrong: %#v", events)
	}
}

func TestStreamEventsFromResponseIncludesTextBeforeConfirmation(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolName: "execute_write",
					Result: map[string]any{
						"status": "pending",
						"token":  "tok",
						"dsl":    "DELETE FROM orders WHERE id = 1",
					},
				}},
			},
		},
	}

	events := streamEventsFromResponse(resp)
	if len(events) != 2 {
		t.Fatalf("expected text and confirmation events, got %#v", events)
	}
	if events[0].Type != ChatStreamEventChunk || events[0].Content != "Please confirm." {
		t.Fatalf("unexpected first event: %#v", events[0])
	}
	if events[1].Type != ChatStreamEventConfirmRequired || events[1].Confirmation == nil {
		t.Fatalf("unexpected confirmation event: %#v", events[1])
	}
}

func TestPrefetchRequestMetadata_DefaultIdentityAndWorkspace(t *testing.T) {
	m := prefetchRequestMetadata("sess-1", "agent-9", "/ws", "")
	if m["identity"] != "sess-1" {
		t.Fatalf("identity fallback mismatch: %#v", m)
	}
	if m["workspace_root"] != "/ws" || m["agent_id"] != "agent-9" || m["session_id"] != "sess-1" {
		t.Fatalf("unexpected metadata: %#v", m)
	}
}
