package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

type fakeBackend struct {
	name string
	out  []PrefetchPart
}

func (f *fakeBackend) Name() string { return f.name }

func (f *fakeBackend) Prefetch(ctx context.Context, q PrefetchQuery) ([]PrefetchPart, error) {
	_ = ctx
	_ = q
	return f.out, nil
}

func TestOrchestrator_PrefetchForTurn_WrapsFenceAndOrigin(t *testing.T) {
	o := NewOrchestrator()
	_ = o.RegisterBackend(&fakeBackend{
		name: "t",
		out:  []PrefetchPart{{Label: "a", Content: "hello recall"}},
	})
	msgs, skip, err := o.PrefetchForTurn(context.Background(), PrefetchQuery{UserMessage: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if skip != PrefetchSkipNone {
		t.Fatalf("unexpected skip: %q", skip)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected system, got %q", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "sixath-memory-context") {
		t.Fatalf("expected fence tag in content: %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "hello recall") {
		t.Fatalf("expected inner body: %q", msgs[0].Content)
	}
	v, ok := msgs[0].Metadata[model.MetadataKeySixathOrigin]
	if !ok || v != model.OriginMemoryFence {
		t.Fatalf("expected sixath.origin=%q, got %#v", model.OriginMemoryFence, msgs[0].Metadata)
	}
}

func TestOrchestrator_PrefetchForTurn_EmptyBackend(t *testing.T) {
	o := NewOrchestrator()
	msgs, skip, err := o.PrefetchForTurn(context.Background(), PrefetchQuery{})
	if err != nil || len(msgs) != 0 || skip != PrefetchSkipNone {
		t.Fatalf("expected empty got msgs=%v skip=%q err=%v", msgs, skip, err)
	}
}

type errBackend struct{ err error }

func (e errBackend) Name() string { return "err" }

func (e errBackend) Prefetch(ctx context.Context, q PrefetchQuery) ([]PrefetchPart, error) {
	_ = ctx
	_ = q
	return nil, e.err
}

func TestOrchestrator_PrefetchForTurn_FailOpenSkipReason(t *testing.T) {
	o := NewOrchestrator()
	_ = o.RegisterBackend(errBackend{err: errors.New("boom")})
	msgs, skip, err := o.PrefetchForTurn(context.Background(), PrefetchQuery{})
	if err != nil || len(msgs) != 0 || skip != PrefetchSkipBackendError {
		t.Fatalf("want backend_error skip, got msgs=%v skip=%q err=%v", msgs, skip, err)
	}
}

func TestOrchestrator_PrefetchForTurn_FailClosedReturnsError(t *testing.T) {
	o := NewOrchestrator()
	o.PrefetchFailClosed = true
	_ = o.RegisterBackend(errBackend{err: errors.New("boom")})
	_, skip, err := o.PrefetchForTurn(context.Background(), PrefetchQuery{})
	if err == nil || skip != PrefetchSkipNone {
		t.Fatalf("want error, got skip=%q err=%v", skip, err)
	}
}

func TestOrchestrator_PrefetchForTurn_TimeoutSkipReason(t *testing.T) {
	o := NewOrchestrator()
	o.PrefetchTimeoutMS = 25
	_ = o.RegisterBackend(&slowBackend{})
	msgs, skip, err := o.PrefetchForTurn(context.Background(), PrefetchQuery{})
	if err != nil {
		t.Fatalf("fail-open: %v", err)
	}
	if len(msgs) != 0 || skip != PrefetchSkipTimeout {
		t.Fatalf("want timeout skip, got msgs=%d skip=%q", len(msgs), skip)
	}
}

type slowBackend struct{}

func (s *slowBackend) Name() string { return "slow" }

func (s *slowBackend) Prefetch(ctx context.Context, q PrefetchQuery) ([]PrefetchPart, error) {
	_ = q
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestOrchestrator_PrefetchForTurn_EmptyPartsSkipReason(t *testing.T) {
	o := NewOrchestrator()
	_ = o.RegisterBackend(&fakeBackend{name: "x", out: nil})
	_, skip, err := o.PrefetchForTurn(context.Background(), PrefetchQuery{UserMessage: "q"})
	if err != nil || skip != PrefetchSkipEmptyParts {
		t.Fatalf("want empty_parts, skip=%q err=%v", skip, err)
	}
}

func TestOrchestrator_RegisterBackend_AtMostOne(t *testing.T) {
	o := NewOrchestrator()
	if err := o.RegisterBackend(&fakeBackend{name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := o.RegisterBackend(&fakeBackend{name: "b"}); err == nil {
		t.Fatal("expected error on second RegisterBackend")
	}
}
