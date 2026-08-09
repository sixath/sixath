package toolmem

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/memory"
	core "github.com/sixath/framework/tool"
)

type recordingStore struct {
	rememberInput memory.RememberInput
	getRef        memory.GetRef
	recallQuery   memory.RecallQuery
	recallHits    []memory.MemoryHit
}

func (s *recordingStore) Remember(_ context.Context, in memory.RememberInput) (memory.MemoryHit, error) {
	s.rememberInput = in
	return memory.MemoryHit{Scope: in.Scope, ID: "remembered"}, nil
}

func (s *recordingStore) Recall(_ context.Context, q memory.RecallQuery) ([]memory.MemoryHit, error) {
	s.recallQuery = q
	if s.recallHits != nil {
		return s.recallHits, nil
	}
	return nil, nil
}

func (s *recordingStore) Get(_ context.Context, ref memory.GetRef) (memory.MemoryHit, error) {
	s.getRef = ref
	return memory.MemoryHit{Scope: ref.Scope, Path: ref.Path, ID: ref.ID}, nil
}

func (s *recordingStore) List(context.Context, memory.ListFilter) ([]memory.MemoryHit, error) {
	return nil, nil
}

func (s *recordingStore) Delete(context.Context, memory.GetRef) error {
	return nil
}

func TestRegisterMemoryStoreToolsRegistersAllTools(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{}); err != nil {
		t.Fatalf("RegisterMemoryStoreTools() error = %v", err)
	}
	for _, name := range []string{"memory_remember", "memory_recall", "memory_get"} {
		registered, ok := reg.Get(name)
		if !ok {
			t.Errorf("%s was not registered", name)
			continue
		}
		if registered.Toolset != core.ToolsetMemory {
			t.Errorf("%s Toolset = %q, want %q", name, registered.Toolset, core.ToolsetMemory)
		}
	}
}

func TestMemoryRemember_UserScopeUsesContextUserID(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	rememberTool, _ := reg.Get("memory_remember")
	recallTool, _ := reg.Get("memory_recall")

	ctx := context.WithValue(context.Background(), core.ContextKeyUserID, "user-1")
	ctx = context.WithValue(ctx, core.ContextKeySessionID, "session-1")

	result, err := rememberTool.Execute(ctx, map[string]any{
		"scope": "user", "action": "add", "content": "likes concise answers",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if _, ok := m["error"]; ok {
		t.Fatalf("result = %#v, want no error", m)
	}
	if m["id"] == "" {
		t.Fatalf("result = %#v, want remembered unit ID", m)
	}
	meta, _ := m["metadata"].(map[string]any)
	if meta["source_session_id"] != "session-1" {
		t.Fatalf("source_session_id = %v, want session-1", meta["source_session_id"])
	}

	recallResult, err := recallTool.Execute(ctx, map[string]any{
		"scope": "user", "query": "concise",
	})
	if err != nil {
		t.Fatalf("recall Execute() error = %v", err)
	}
	recallMap := recallResult.(map[string]any)
	if _, ok := recallMap["error"]; ok {
		t.Fatalf("recall result = %#v, want no error", recallMap)
	}
	hits, _ := recallMap["hits"].([]map[string]any)
	if len(hits) != 1 {
		t.Fatalf("recall hits = %#v, want 1 hit", recallMap["hits"])
	}
}

func TestMemoryRemember_UserScopeSilentWithoutUserID(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_remember")
	result, err := tl.Execute(context.Background(), map[string]any{
		"scope": "user", "action": "add", "content": "fact",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if _, ok := m["error"]; ok {
		t.Fatalf("result = %#v, want no error key", m)
	}
	if m["skipped"] != true || m["reason"] != "user_id_missing" {
		t.Fatalf("result = %#v, want skipped with user_id_missing", m)
	}
}

func TestMemoryRecall_UserScopeEmptyWithoutUserID(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_recall")
	result, err := tl.Execute(context.Background(), map[string]any{
		"scope": "user", "query": "fact",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if _, ok := m["error"]; ok {
		t.Fatalf("result = %#v, want no error key", m)
	}
	hits, _ := m["hits"].([]map[string]any)
	if hits == nil {
		hits = []map[string]any{}
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %#v, want empty slice", m["hits"])
	}
}

func TestMemoryGet_UserScopeNotFoundWithoutUserID(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_get")
	result, err := tl.Execute(context.Background(), map[string]any{
		"scope": "user", "id": "unit-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["error"] != "not_found" {
		t.Fatalf("result = %#v, want not_found", m)
	}
}

func TestMemoryRememberSessionUsesContextSessionID(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_remember")
	ctx := context.WithValue(context.Background(), core.ContextKeySessionID, "session-1")
	result, err := tl.Execute(ctx, map[string]any{
		"scope":   "session",
		"action":  "add",
		"content": "Remember this deployment detail.",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.(map[string]any)["id"] == "" {
		t.Fatalf("result = %#v, want remembered unit ID", result)
	}
}

func TestMemoryRememberAgentTargets(t *testing.T) {
	for _, target := range []string{"memory", "user_file"} {
		t.Run(target, func(t *testing.T) {
			store := &recordingStore{}
			reg := core.NewRegistry()
			if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{AgentWriteEnabled: true}); err != nil {
				t.Fatal(err)
			}
			tl, _ := reg.Get("memory_remember")
			ctx := context.WithValue(context.Background(), core.ContextKeyWorkspaceRoot, t.TempDir())
			ctx = context.WithValue(ctx, core.ContextKeyAgentID, "agent-1")
			_, err := tl.Execute(ctx, map[string]any{
				"scope":   "agent",
				"action":  "add",
				"target":  target,
				"content": "Durable fact",
			})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if store.rememberInput.Scope != memory.ScopeAgent || store.rememberInput.Target != target {
				t.Fatalf("RememberInput = %#v, want agent target %q", store.rememberInput, target)
			}
		})
	}
}

func TestMemoryRememberAgentRequiresWorkspaceRoot(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{AgentWriteEnabled: true}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_remember")
	result, err := tl.Execute(context.Background(), map[string]any{
		"scope": "agent", "action": "add", "target": "memory", "content": "fact",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.(map[string]any)["error"] != "workspace_root_missing" {
		t.Fatalf("result = %#v, want workspace_root_missing", result)
	}
}

func TestMemoryGetRejectsPathEscapeAndAllowsMemoryMD(t *testing.T) {
	store := &recordingStore{}
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_get")
	ctx := context.WithValue(context.Background(), core.ContextKeyWorkspaceRoot, t.TempDir())

	result, err := tl.Execute(ctx, map[string]any{"scope": "agent", "path": "../secret.md"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.(map[string]any)["error"] != "path_not_allowed" {
		t.Fatalf("result = %#v, want path_not_allowed", result)
	}

	_, err = tl.Execute(ctx, map[string]any{"scope": "agent", "path": "MEMORY.md"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.getRef.Path != "MEMORY.md" {
		t.Fatalf("GetRef.Path = %q, want MEMORY.md", store.getRef.Path)
	}
}

func TestMemoryStoreToolsRejectNilDependencies(t *testing.T) {
	if err := RegisterMemoryStoreTools(nil, &recordingStore{}, StoreToolsOptions{}); err == nil {
		t.Fatal("nil registry did not return an error")
	}
	if err := RegisterMemoryStoreTools(core.NewRegistry(), nil, StoreToolsOptions{}); err == nil {
		t.Fatal("nil store did not return an error")
	} else if errors.Is(err, memory.ErrScopeNotEnabled) {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestMemoryRecallSchemaQueryOptionalForTranscript(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_recall")
	params, ok := tl.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("Parameters type = %T, want map[string]any", tl.Parameters)
	}
	required, _ := params["required"].([]string)
	for _, key := range required {
		if key == "query" {
			t.Fatalf("required = %#v, query must not be required", required)
		}
	}
	if len(required) != 1 || required[0] != "scope" {
		t.Fatalf("required = %#v, want [scope]", required)
	}
	props, _ := params["properties"].(map[string]any)
	for _, key := range []string{"anchor_window", "include_tools", "exclude_current"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("missing property %q in %#v", key, props)
		}
	}
}

func TestMemoryRecallTranscriptEmptyReturnsSessions(t *testing.T) {
	store := &recordingStore{
		recallHits: []memory.MemoryHit{{
			Scope:   memory.ScopeSession,
			Source:  memory.SourceTranscript,
			ID:      "s1",
			Content: "preview text",
			Metadata: map[string]any{
				"session_id": "s1", "title": "Past", "list_recent": true,
			},
		}},
	}
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_recall")
	ctx := context.WithValue(context.Background(), core.ContextKeySessionID, "current")
	ctx = context.WithValue(ctx, core.ContextKeyAgentID, "agent-1")

	result, err := tl.Execute(ctx, map[string]any{
		"scope": "session", "source": "transcript",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Fatalf("result = %#v, want count 1", m)
	}
	sessions, _ := m["sessions"].([]map[string]any)
	if len(sessions) != 1 || sessions[0]["session_id"] != "s1" || sessions[0]["title"] != "Past" {
		t.Fatalf("sessions = %#v", m["sessions"])
	}
	if store.recallQuery.Source != memory.SourceTranscript || store.recallQuery.Query != "" {
		t.Fatalf("RecallQuery = %#v", store.recallQuery)
	}
}

func TestMemoryRecallTranscriptAnchoredShape(t *testing.T) {
	store := &recordingStore{
		recallHits: []memory.MemoryHit{{
			Scope:  memory.ScopeSession,
			Source: memory.SourceTranscript,
			ID:     "s1",
			Score:  2,
			Metadata: map[string]any{
				"session_id": "s1",
				"title":      "Anchored",
				"anchor":     map[string]any{"id": "m1", "role": "user", "content": "redis"},
				"window":     []map[string]any{{"id": "m1", "content": "redis"}},
				"bookend_start": []map[string]any{},
				"bookend_end":   []map[string]any{},
				"anchored":      true,
			},
		}},
	}
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_recall")
	ctx := context.WithValue(context.Background(), core.ContextKeySessionID, "current")

	includeTools := false
	result, err := tl.Execute(ctx, map[string]any{
		"scope": "session", "source": "transcript", "query": "redis",
		"anchor_window": 4, "include_tools": includeTools, "exclude_current": false,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Fatalf("result = %#v", m)
	}
	hits, _ := m["hits"].([]map[string]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %#v", m["hits"])
	}
	if hits[0]["session_id"] != "s1" || hits[0]["title"] != "Anchored" {
		t.Fatalf("hit = %#v", hits[0])
	}
	anchor, _ := hits[0]["anchor"].(map[string]any)
	if anchor["content"] != "redis" {
		t.Fatalf("anchor = %#v", anchor)
	}
	if _, ok := hits[0]["window"]; !ok {
		t.Fatalf("missing window in %#v", hits[0])
	}
	if store.recallQuery.AnchorWindow != 4 {
		t.Fatalf("AnchorWindow = %d", store.recallQuery.AnchorWindow)
	}
	if store.recallQuery.IncludeTools == nil || *store.recallQuery.IncludeTools {
		t.Fatalf("IncludeTools = %#v, want false", store.recallQuery.IncludeTools)
	}
	if store.recallQuery.ExcludeCurrent == nil || *store.recallQuery.ExcludeCurrent {
		t.Fatalf("ExcludeCurrent = %#v, want false", store.recallQuery.ExcludeCurrent)
	}
}

func TestMemoryRecallUnitsEmptyQueryRejected(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, &recordingStore{}, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("memory_recall")
	ctx := context.WithValue(context.Background(), core.ContextKeySessionID, "s1")
	result, err := tl.Execute(ctx, map[string]any{"scope": "session", "source": "units"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.(map[string]any)["error"] != "empty_query_rejected" {
		t.Fatalf("result = %#v, want empty_query_rejected", result)
	}
}
