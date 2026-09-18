package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/turntrace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openForkDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChatSession{}, &model.ChatMessage{}, &model.TurnTraceRow{}, &MemoryUnit{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type seededParent struct {
	sess *biz.ChatSession
	msgs []*biz.ChatMessage
}

func seedParent(t *testing.T, db *gorm.DB, base time.Time) seededParent {
	t.Helper()
	ctx := context.Background()
	sess, err := (&chatSessionRepo{db: db}).Create(ctx, "u1", "a1", "parent chat", "")
	if err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"m0", "m1", "m2"} {
		m := &model.ChatMessage{
			ID:        content,
			SessionID: sess.ID,
			Role:      "user",
			Content:   content,
			Active:    true,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := (&chatMessageRepo{db: db}).ListActiveOrdered(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("seed msgs len=%d want 3", len(msgs))
	}
	store := NewTurnTraceStore(db)
	if err := store.Upsert(ctx, &agent.TurnTrace{
		SessionID: sess.ID,
		AgentID:   sess.AgentID,
		RequestID: "req-early",
		CreatedAt: base,
		Calls:     []agent.TurnToolCall{{ToolName: "read"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, &agent.TurnTrace{
		SessionID: sess.ID,
		AgentID:   sess.AgentID,
		RequestID: "req-late",
		CreatedAt: base.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionUnitsBackend(db).Remember(ctx, memory.RememberInput{
		Scope:    memory.ScopeSession,
		ScopeID:  sess.ID,
		AgentID:  sess.AgentID,
		Action:   memory.ActionAdd,
		Content:  "session fact",
		Metadata: map[string]any{"k": "v"},
	}); err != nil {
		t.Fatal(err)
	}
	return seededParent{sess: sess, msgs: msgs}
}

func TestForkSnapshot_CopiesPrefixTracesAndMemory(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	parent := seedParent(t, db, base)
	var out ForkSnapshotResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		out, e = ForkSnapshot(ctx, tx, ForkSnapshotInput{
			Parent: parent.sess,
			Anchor: parent.msgs[1],
		})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.CopiedMessages != 2 || out.CopiedTraces != 1 || out.CopiedMemoryUnits != 1 {
		t.Fatalf("counts %+v", out)
	}
	childMsgs, _ := (&chatMessageRepo{db: db}).ListActiveOrdered(ctx, out.Child.ID)
	if len(childMsgs) != 2 || childMsgs[1].Content != parent.msgs[1].Content {
		t.Fatalf("child msgs %+v", childMsgs)
	}
	parentMsgs, _ := (&chatMessageRepo{db: db}).ListActiveOrdered(ctx, parent.sess.ID)
	if len(parentMsgs) != 3 {
		t.Fatal("parent must keep all messages")
	}
	childTraces, err := NewTurnTraceStore(db).ListBySession(ctx, out.Child.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range childTraces {
		if tr.SessionID != out.Child.ID {
			t.Fatalf("child trace session_id=%s want %s", tr.SessionID, out.Child.ID)
		}
	}
}

func TestForkSnapshot_CopiesModelOverlay(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	parent := seedParent(t, db, base)
	sessRepo := &chatSessionRepo{db: db}
	if err := sessRepo.SetModelOverride(ctx, parent.sess.ID, "prov-1", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	gotParent, err := sessRepo.GetByID(ctx, parent.sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	parent.sess = gotParent
	var out ForkSnapshotResult
	err = db.Transaction(func(tx *gorm.DB) error {
		var e error
		out, e = ForkSnapshot(ctx, tx, ForkSnapshotInput{
			Parent: parent.sess,
			Anchor: parent.msgs[1],
		})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := sessRepo.GetByID(ctx, out.Child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ModelProviderID != "prov-1" || child.Model != "gpt-4o" {
		t.Fatalf("child overlay %+v", child)
	}
}

func TestForkSnapshot_TraceTurnSeqChronological(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	parent := seedParent(t, db, base)

	var out ForkSnapshotResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		out, e = ForkSnapshot(ctx, tx, ForkSnapshotInput{
			Parent: parent.sess,
			Anchor: parent.msgs[2],
		})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := NewTurnTraceStore(db).ListBySession(ctx, out.Child.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("child traces len=%d want 2", len(list))
	}
	earlier, later := list[0], list[1]
	if later.CreatedAt.Before(earlier.CreatedAt) {
		earlier, later = later, earlier
	}
	if earlier.TurnSeq >= later.TurnSeq {
		t.Fatalf("earlier CreatedAt=%s seq=%d; later CreatedAt=%s seq=%d; want earlier seq < later seq",
			earlier.CreatedAt, earlier.TurnSeq, later.CreatedAt, later.TurnSeq)
	}
}

func TestForkSnapshot_RollsBackOnTraceError(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	parent := seedParent(t, db, time.Now().UTC())
	err := db.Transaction(func(tx *gorm.DB) error {
		failing := ForkDeps{
			Sessions: &chatSessionRepo{db: tx},
			Messages: &chatMessageRepo{db: tx},
			Traces:   failTraceStore{},
			Units:    NewSessionUnitsBackend(tx),
		}
		_, e := ForkSnapshotWithDeps(ctx, tx, ForkSnapshotInput{Parent: parent.sess, Anchor: parent.msgs[0]}, failing)
		return e
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var n int64
	db.Model(&model.ChatSession{}).Count(&n)
	if n != 1 {
		t.Fatalf("sessions=%d want 1 (parent only)", n)
	}
}

func TestForkSnapshot_SkipsInactiveMessages(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	parent := seedParent(t, db, base)
	hidden := &model.ChatMessage{
		ID:        "hidden",
		SessionID: parent.sess.ID,
		Role:      "user",
		Content:   "hidden",
		Active:    true,
		CreatedAt: base.Add(500 * time.Millisecond),
	}
	if err := db.Create(hidden).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChatMessage{}).Where("id = ?", "hidden").Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	var out ForkSnapshotResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		out, e = ForkSnapshot(ctx, tx, ForkSnapshotInput{
			Parent: parent.sess,
			Anchor: parent.msgs[1],
		})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	childMsgs, err := (&chatMessageRepo{db: db}).ListActiveOrdered(ctx, out.Child.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range childMsgs {
		if m.Content == "hidden" || m.ID == "hidden" {
			t.Fatalf("child must not include inactive message: %+v", childMsgs)
		}
	}
	var childAll []model.ChatMessage
	if err := db.Where("session_id = ?", out.Child.ID).Find(&childAll).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range childAll {
		if m.Content == "hidden" {
			t.Fatalf("child rows include hidden: %+v", childAll)
		}
	}
	got, err := (&chatMessageRepo{db: db}).GetByID(ctx, "hidden")
	if err != nil {
		t.Fatal(err)
	}
	if got.Active {
		t.Fatal("parent inactive row must stay hidden")
	}
}

func TestForkSnapshot_ReadonlyParentCreatesWritableChild(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	parent := seedParent(t, db, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	if err := (&chatSessionRepo{db: db}).MarkReadonly(ctx, parent.sess.ID); err != nil {
		t.Fatal(err)
	}

	var out ForkSnapshotResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		out, e = ForkSnapshot(ctx, tx, ForkSnapshotInput{
			Parent: parent.sess,
			Anchor: parent.msgs[1],
		})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Child == nil || out.Child.Readonly {
		t.Fatalf("child must be writable, got %+v", out.Child)
	}
	got, err := (&chatSessionRepo{db: db}).GetByID(ctx, parent.sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Readonly {
		t.Fatal("parent must stay readonly")
	}
}

func TestForkSnapshot_AnchorNotInActivePrefix(t *testing.T) {
	db := openForkDB(t)
	ctx := context.Background()
	parent := seedParent(t, db, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	err := db.Transaction(func(tx *gorm.DB) error {
		_, e := ForkSnapshot(ctx, tx, ForkSnapshotInput{
			Parent: parent.sess,
			Anchor: &biz.ChatMessage{ID: "missing-anchor"},
		})
		return e
	})
	if !errors.Is(err, ErrForkAnchorMissing) {
		t.Fatalf("err=%v want ErrForkAnchorMissing", err)
	}
}

type failTraceStore struct{}

var _ turntrace.Store = failTraceStore{}

func (failTraceStore) Upsert(context.Context, *agent.TurnTrace) error {
	return errors.New("boom")
}

func (failTraceStore) GetByRequest(context.Context, string, string) (*agent.TurnTrace, error) {
	return nil, nil
}

func (failTraceStore) ListBySession(context.Context, string, int) ([]agent.TurnTrace, error) {
	// Dummy row so ForkSnapshot reaches Upsert (empty list would skip the error path).
	return []agent.TurnTrace{{SessionID: "p", RequestID: "r"}}, nil
}

func (failTraceStore) DeactivateAfter(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}

func (failTraceStore) ListByAgent(context.Context, string, time.Time, time.Time, int) ([]agent.TurnTrace, error) {
	return nil, nil
}
