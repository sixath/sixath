package cases

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repairCase() Case {
	return Case{
		Symptom:   "vm 225781 预启动一直失败，游戏启动超时 cloud_game_init_finish 不出现",
		Signature: []string{"cloud_game_init_finish", "check.bat", "预启动超时", "yysls"},
		Chain: []ChainLink{
			{Kind: "root", Statement: "宿主机重启打断补丁，Patch\\repair 标记残留"},
			{Kind: "mechanism", Statement: "cgvmagent 180 秒超时杀进程"},
		},
		VerifyProbes: []Probe{{Tool: "vm_run_cmd", Args: map[string]any{"op": "ls_recent", "path": `G:\yysls\LocalData\Patch`}, Expect: "repair 文件存在"}},
		Fix:          "摘出资源池手动跑完修复",
	}
}

func TestFileStoreLifecycle(t *testing.T) {
	s := NewFileStore(t.TempDir())
	draft, err := s.SaveDraft(repairCase())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != StatusDraft || draft.ID == "" {
		t.Fatalf("draft = %+v", draft)
	}
	q := "vm 255266 预启动失败 cloud_game_init_finish 一直没有 check.bat 超时"
	if hits, _ := s.Search(q, 3, false); len(hits) != 0 {
		t.Fatalf("drafts must not be recalled: %+v", hits)
	}
	if hits, _ := s.Search(q, 3, true); len(hits) != 1 {
		t.Fatalf("includeDrafts should find the draft: %+v", hits)
	}
	fix := "重新部署游戏盘"
	if _, err := s.Update(draft.ID, Patch{Fix: &fix}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Confirm(draft.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusConfirmed || got.ConfirmedBy != "alice" || got.Fix != fix || got.ConfirmedAt == nil {
		t.Fatalf("confirmed = %+v", got)
	}
	hits, err := s.Search(q, 3, false)
	if err != nil || len(hits) != 1 || hits[0].Case.ID != draft.ID {
		t.Fatalf("hits = %+v err=%v", hits, err)
	}
	if hits[0].Case.VerifyProbes[0].Args["op"] != "ls_recent" {
		t.Fatalf("probe args lost: %+v", hits[0].Case.VerifyProbes)
	}
	if _, err := s.SaveDraft(Case{ID: draft.ID, Symptom: "x"}); err == nil {
		t.Fatal("a confirmed case must not be overwritten by a draft")
	}
	if err := s.Discard(draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after discard err = %v", err)
	}
}

func TestSearchNeedsSeveralSharedTerms(t *testing.T) {
	s := NewFileStore(t.TempDir())
	c, _ := s.SaveDraft(repairCase())
	_, _ = s.Confirm(c.ID, "")
	if hits, _ := s.Search("数据库连接失败", 3, false); len(hits) != 0 {
		t.Fatalf("unrelated symptom recalled: %+v", hits)
	}
}

func TestLoadIgnoresBodyAndAcceptsCRLF(t *testing.T) {
	dir := t.TempDir()
	body := "---\r\nstatus: confirmed\r\nsymptom: disk full\r\n---\r\n\r\n# anything\r\n"
	if err := os.WriteFile(filepath.Join(dir, "manual-1.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewFileStore(dir).Get("manual-1")
	if err != nil || c.Symptom != "disk full" || c.Status != StatusConfirmed || c.ID != "manual-1" {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestInvalidIDRejected(t *testing.T) {
	if _, err := NewFileStore(t.TempDir()).Get("../x"); err == nil {
		t.Fatal("path traversal id must be rejected")
	}
}

func TestTokenize(t *testing.T) {
	got := strings.Join(tokenize("预启动 check.bat 失败"), "|")
	if got != "预启|启动|check|bat|失败" {
		t.Fatalf("tokenize = %s", got)
	}
}
