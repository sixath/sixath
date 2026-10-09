package biz

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mkGitRepo(t *testing.T, dir, head string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverGitRepos_StopsAtGitRootAndSkipsNoise(t *testing.T) {
	root := t.TempDir()
	mkGitRepo(t, filepath.Join(root, "cloudgame", "svc-a"), "ref: refs/heads/main\n")
	mkGitRepo(t, filepath.Join(root, "cloudgame", "svc-b"), "ref: refs/heads/main\n")
	mkGitRepo(t, filepath.Join(root, "solo"), "ref: refs/heads/main\n")
	// nested repo inside svc-a must not be reported
	mkGitRepo(t, filepath.Join(root, "cloudgame", "svc-a", "third_party", "x"), "ref: refs/heads/main\n")
	mkGitRepo(t, filepath.Join(root, "web", "node_modules", "pkg"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(root, "notes", "readme.md"), "x")

	got, err := DiscoverGitRepos(root, 32)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cloudgame/svc-a", "cloudgame/svc-b", "solo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverGitRepos_RespectsDepth(t *testing.T) {
	root := t.TempDir()
	mkGitRepo(t, filepath.Join(root, "a", "b", "c"), "ref: refs/heads/main\n")
	got, _ := DiscoverGitRepos(root, 2)
	if len(got) != 0 {
		t.Fatalf("depth 2 should not reach a/b/c, got %v", got)
	}
	got, _ = DiscoverGitRepos(root, 3)
	if !reflect.DeepEqual(got, []string{"a/b/c"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiscoverGitRepos_GitFileCountsAsRepo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "wt", ".git"), "gitdir: ../main/.git/worktrees/wt\n")
	got, _ := DiscoverGitRepos(root, 32)
	if !reflect.DeepEqual(got, []string{"wt"}) {
		t.Fatalf("got %v", got)
	}
}

func TestReadGitInfo_LooseRef(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/feature/x\n")
	mustWrite(t, filepath.Join(repo, ".git", "refs", "heads", "feature", "x"), "abc123\n")
	mustWrite(t, filepath.Join(repo, ".git", "config"),
		"[core]\n\tbare = false\n[remote \"origin\"]\n\turl = https://bot:s3cret@git.example.com/g/r.git\n")
	info, err := ReadGitInfo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "feature/x" || info.Commit != "abc123" {
		t.Fatalf("info = %#v", info)
	}
	if info.Remote != "https://git.example.com/g/r.git" {
		t.Fatalf("remote must drop credentials, got %q", info.Remote)
	}
}

func TestReadGitInfo_PackedRef(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(repo, ".git", "packed-refs"),
		"# pack-refs with: peeled\ndef456 refs/heads/main\n^aaa\n")
	info, _ := ReadGitInfo(repo)
	if info.Commit != "def456" {
		t.Fatalf("commit = %q", info.Commit)
	}
}

func TestReadGitInfo_DetachedHead(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "0123abcd\n")
	info, _ := ReadGitInfo(repo)
	if info.Branch != "" || info.Commit != "0123abcd" {
		t.Fatalf("info = %#v", info)
	}
}

func TestReadGitInfo_WorktreeGitFile(t *testing.T) {
	base := t.TempDir()
	mainGit := filepath.Join(base, "main", ".git")
	wtGit := filepath.Join(mainGit, "worktrees", "wt")
	mustWrite(t, filepath.Join(wtGit, "HEAD"), "ref: refs/heads/wt-branch\n")
	mustWrite(t, filepath.Join(wtGit, "commondir"), "../..\n")
	mustWrite(t, filepath.Join(mainGit, "refs", "heads", "wt-branch"), "fff000\n")
	mustWrite(t, filepath.Join(base, "wt", ".git"), "gitdir: "+wtGit+"\n")

	info, err := ReadGitInfo(filepath.Join(base, "wt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "wt-branch" || info.Commit != "fff000" {
		t.Fatalf("info = %#v", info)
	}
}
