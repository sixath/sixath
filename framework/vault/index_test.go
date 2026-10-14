package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/memory"
)

func TestSyncUpsertAndDelete(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "ops/a.md", "---\nstatus: promoted\n---\n\nbodyA")
	idx := memory.NewInMemoryUnitVectorIndex()

	rels, err := Sync(context.Background(), fakeEmbedder{}, idx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	has, err := idx.Has(context.Background(), "vault", "vault", []string{"ops/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !has["ops/a.md"] {
		t.Fatalf("expected ops/a.md indexed")
	}

	// 删除笔记后再次 Sync（传上次 rels），应清掉旧 unitID。
	if err := os.Remove(filepath.Join(dir, "ops", "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), fakeEmbedder{}, idx, dir, rels); err != nil {
		t.Fatal(err)
	}
	has, _ = idx.Has(context.Background(), "vault", "vault", []string{"ops/a.md"})
	if has["ops/a.md"] {
		t.Fatalf("expected ops/a.md removed after delete")
	}
}
