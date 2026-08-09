package chat

import (
	"context"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

func testMemoryUnitWriter(t *testing.T) local.UnitWriter {
	t.Helper()
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	return NewMemoryUnitWriter(store)
}

func TestMemoryUnitWriter_WriteListApprove(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	uw := NewMemoryUnitWriter(store)
	ctx := context.Background()
	agentID := "agent-uw-1"

	writeID, err := uw.WriteDraft(ctx, agentID, "", "Note", "draft body secret")
	if err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}
	if writeID == "" {
		t.Fatal("empty id")
	}

	drafts, err := uw.ListDrafts(ctx, agentID, 10)
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(drafts) != 1 || drafts[0].ID != writeID {
		t.Fatalf("ListDrafts=%+v want id=%s", drafts, writeID)
	}
	if drafts[0].Title != "Note" || !strings.Contains(drafts[0].Preview, "draft body") {
		t.Fatalf("meta=%+v", drafts[0])
	}

	if err := uw.ApproveDraft(ctx, agentID, writeID); err != nil {
		t.Fatalf("ApproveDraft: %v", err)
	}
	drafts, err = uw.ListDrafts(ctx, agentID, 10)
	if err != nil {
		t.Fatalf("ListDrafts after approve: %v", err)
	}
	if len(drafts) != 0 {
		t.Fatalf("want empty drafts, got %+v", drafts)
	}

	hit, err := store.Get(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: agentID, ID: writeID, AgentID: agentID,
	})
	if err != nil {
		t.Fatalf("Get after approve: %v", err)
	}
	if hit.ID != writeID {
		t.Fatalf("approve changed unit id: got %q want %q", hit.ID, writeID)
	}
	if local.MapUnitToAssetStatus(unitDBStatus(hit.Metadata), hit.Metadata) != hub.AssetActive {
		t.Fatalf("want AssetActive, meta=%+v", hit.Metadata)
	}
	if _, ok := hit.Metadata["hub_status"]; ok {
		t.Fatalf("hub_status key should be absent: %+v", hit.Metadata)
	}
}

func TestMemoryUnitWriter_ApproveMapsActive(t *testing.T) {
	sess := memory.NewSessionMemory()
	store := memory.NewFacade(memory.FacadeConfig{Session: sess})
	uw := NewMemoryUnitWriter(store)
	ctx := context.Background()
	agentID := "agent-uw-2"

	writeID, err := uw.WriteDraft(ctx, agentID, "", "T", "body")
	if err != nil {
		t.Fatal(err)
	}
	if err := uw.ApproveDraft(ctx, agentID, writeID); err != nil {
		t.Fatal(err)
	}

	hit, err := store.Get(ctx, memory.GetRef{
		Scope:   memory.ScopeUser,
		ScopeID: agentID,
		ID:      writeID,
		AgentID: agentID,
	})
	if err != nil {
		t.Fatalf("Get after approve: %v", err)
	}
	if hit.ID != writeID {
		t.Fatalf("approve changed unit id: got %q want %q", hit.ID, writeID)
	}
	if local.MapUnitToAssetStatus(unitDBStatus(hit.Metadata), hit.Metadata) != hub.AssetActive {
		t.Fatalf("want AssetActive, meta=%+v", hit.Metadata)
	}
	if _, ok := hit.Metadata["hub_status"]; ok {
		t.Fatalf("hub_status key should be absent: %+v", hit.Metadata)
	}
	if hit.Content != "body" {
		t.Fatalf("content=%q", hit.Content)
	}
}

func TestMemoryUnitWriter_EmptyAgentID(t *testing.T) {
	uw := testMemoryUnitWriter(t)
	ctx := context.Background()
	if _, err := uw.WriteDraft(ctx, "", "", "t", "c"); err == nil {
		t.Fatal("expected empty agent id error")
	}
	if _, err := uw.WriteDraft(ctx, "a", "", "t", ""); err == nil {
		t.Fatal("expected empty content error")
	}
	if err := uw.ApproveDraft(ctx, "", "id"); err == nil {
		t.Fatal("expected empty agent id on approve")
	}
	if _, err := uw.ListDrafts(ctx, "", 5); err == nil {
		t.Fatal("expected empty agent id on list")
	}
}

func TestMemoryUnitWriter_RejectOverwriteActive(t *testing.T) {
	sess := memory.NewSessionMemory()
	store := memory.NewFacade(memory.FacadeConfig{Session: sess})
	uw := NewMemoryUnitWriter(store)
	ctx := context.Background()
	agentID := "agent-uw-3"

	writeID, err := uw.WriteDraft(ctx, agentID, "", "T", "draft")
	if err != nil {
		t.Fatal(err)
	}
	if err := uw.ApproveDraft(ctx, agentID, writeID); err != nil {
		t.Fatal(err)
	}
	hit, err := store.Get(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: agentID, ID: writeID, AgentID: agentID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID != writeID {
		t.Fatalf("approve changed id: %q != %q", hit.ID, writeID)
	}
	if local.MapUnitToAssetStatus(unitDBStatus(hit.Metadata), hit.Metadata) != hub.AssetActive {
		t.Fatalf("want active, meta=%+v", hit.Metadata)
	}
	if _, err := uw.WriteDraft(ctx, agentID, writeID, "T", "overwrite"); err == nil {
		t.Fatal("expected reject overwrite of active unit")
	}
}

func TestMemoryUnitWriter_UpdateDraft(t *testing.T) {
	sess := memory.NewSessionMemory()
	store := memory.NewFacade(memory.FacadeConfig{Session: sess})
	uw := NewMemoryUnitWriter(store)
	ctx := context.Background()
	agentID := "agent-uw-4"
	writeID, err := uw.WriteDraft(ctx, agentID, "", "T", "v1")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := uw.WriteDraft(ctx, agentID, writeID, "T2", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if id2 != writeID {
		t.Fatalf("update draft changed id: got %q want %q", id2, writeID)
	}
	hit, err := store.Get(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: agentID, ID: writeID, AgentID: agentID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID != writeID {
		t.Fatalf("Get id=%q want %q", hit.ID, writeID)
	}
	if hit.Content != "v2" {
		t.Fatalf("content=%q", hit.Content)
	}
	if local.MapUnitToAssetStatus(unitDBStatus(hit.Metadata), hit.Metadata) != hub.AssetDraft {
		t.Fatalf("want still draft, meta=%+v", hit.Metadata)
	}
	drafts, err := uw.ListDrafts(ctx, agentID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != writeID || !strings.Contains(drafts[0].Preview, "v2") {
		t.Fatalf("%+v", drafts)
	}
}

func TestSetHubUnitWriter_DescribeToolsKnowledgeWrite(t *testing.T) {
	ResetLocalMemoryHubForTest()
	t.Cleanup(ResetLocalMemoryHubForTest)

	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	SetHubUnitWriter(NewMemoryUnitWriter(store))
	InitLocalMemoryHub()

	_, know, err := ResolveForAgent(hub.AgentHubConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !know.Capabilities().Write || !know.Capabilities().Has("knowledge_write") {
		t.Fatalf("capabilities=%#v", know.Capabilities())
	}
	names := make([]string, 0, len(know.DescribeTools()))
	for _, d := range know.DescribeTools() {
		names = append(names, d.Name)
	}
	hasWrite, hasApprove := false, false
	for _, n := range names {
		if n == "knowledge_write" {
			hasWrite = true
		}
		if n == "knowledge_approve" {
			hasApprove = true
		}
	}
	if !hasWrite || !hasApprove {
		t.Fatalf("tools=%v", names)
	}
}

type gatedAgentGetter struct {
	meta *biz.AgentMeta
	err  error
}

func (s gatedAgentGetter) Get(context.Context, string) (*biz.AgentMeta, error) {
	return s.meta, s.err
}

func TestGatedMemoryUnitWriter_RequiresMemoryWriteEnabled(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	ctx := context.Background()
	agentID := "agent-gated-1"

	off := NewGatedMemoryUnitWriter(store, gatedAgentGetter{
		meta: &biz.AgentMeta{RuntimeTools: biz.RuntimeToolsConfig{MemoryWriteEnabled: false}},
	})
	if _, err := off.WriteDraft(ctx, agentID, "", "t", "body"); err == nil {
		t.Fatal("expected memory write disabled error")
	}

	on := NewGatedMemoryUnitWriter(store, gatedAgentGetter{
		meta: &biz.AgentMeta{RuntimeTools: biz.RuntimeToolsConfig{MemoryWriteEnabled: true}},
	})
	id, err := on.WriteDraft(ctx, agentID, "", "t", "body")
	if err != nil || id == "" {
		t.Fatalf("WriteDraft enabled: id=%q err=%v", id, err)
	}
	if err := on.ApproveDraft(ctx, agentID, id); err != nil {
		t.Fatalf("ApproveDraft: %v", err)
	}
}
