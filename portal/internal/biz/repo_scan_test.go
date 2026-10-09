package biz

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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

	got, unreadable, err := DiscoverGitRepos(root, 32)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cloudgame/svc-a", "cloudgame/svc-b", "solo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(unreadable) != 0 {
		t.Fatalf("unreadable = %v", unreadable)
	}
}

func TestDiscoverGitRepos_RespectsDepth(t *testing.T) {
	root := t.TempDir()
	mkGitRepo(t, filepath.Join(root, "a", "b", "c"), "ref: refs/heads/main\n")
	got, _, _ := DiscoverGitRepos(root, 2)
	if len(got) != 0 {
		t.Fatalf("depth 2 should not reach a/b/c, got %v", got)
	}
	got, _, _ = DiscoverGitRepos(root, 3)
	if !reflect.DeepEqual(got, []string{"a/b/c"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiscoverGitRepos_GitFileCountsAsRepo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "wt", ".git"), "gitdir: ../main/.git/worktrees/wt\n")
	got, _, _ := DiscoverGitRepos(root, 32)
	if !reflect.DeepEqual(got, []string{"wt"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiscoverGitRepos_SymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	mkGitRepo(t, filepath.Join(real, "a"), "ref: refs/heads/main\n")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	got, _, err := DiscoverGitRepos(link, 32)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiscoverGitRepos_ReportsUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not restrict directory reads on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	mkGitRepo(t, filepath.Join(root, "ok"), "ref: refs/heads/main\n")
	locked := filepath.Join(root, "team", "locked")
	mkGitRepo(t, filepath.Join(locked, "inner"), "ref: refs/heads/main\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	got, unreadable, err := DiscoverGitRepos(root, 32)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"ok"}) {
		t.Fatalf("repos = %v", got)
	}
	if !reflect.DeepEqual(unreadable, []string{"team/locked"}) {
		t.Fatalf("unreadable = %v", unreadable)
	}
}

func TestUnderAnyPrefix(t *testing.T) {
	cases := []struct {
		rel      string
		prefixes []string
		want     bool
	}{
		{"cg/a", []string{"cg"}, true},
		{"cg", []string{"cg"}, true},
		{"cgx/a", []string{"cg"}, false},
		{"solo", []string{"cg", "web"}, false},
		{"anything/deep", []string{""}, true},
		{"x", nil, false},
	}
	for _, c := range cases {
		if got := underAnyPrefix(c.rel, c.prefixes); got != c.want {
			t.Fatalf("underAnyPrefix(%q, %v) = %v, want %v", c.rel, c.prefixes, got, c.want)
		}
	}
}

func TestReadGitInfo_LooseRef(t *testing.T) {
	sha := strings.Repeat("a", 40)
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/feature/x\n")
	mustWrite(t, filepath.Join(repo, ".git", "refs", "heads", "feature", "x"), sha+"\n")
	mustWrite(t, filepath.Join(repo, ".git", "config"),
		"[core]\n\tbare = false\n[remote \"origin\"]\n\turl = https://bot:s3cret@git.example.com/g/r.git\n")
	info, err := ReadGitInfo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "feature/x" || info.Commit != sha {
		t.Fatalf("info = %#v", info)
	}
	if info.Remote != "https://git.example.com/g/r.git" {
		t.Fatalf("remote must drop credentials, got %q", info.Remote)
	}
}

func TestReadGitInfo_PackedRef(t *testing.T) {
	sha := strings.Repeat("b", 64)
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(repo, ".git", "packed-refs"),
		"# pack-refs with: peeled\n"+sha+" refs/heads/main\n^aaa\n")
	info, _ := ReadGitInfo(repo)
	if info.Commit != sha {
		t.Fatalf("commit = %q", info.Commit)
	}
}

func TestReadGitInfo_DetachedHead(t *testing.T) {
	sha := strings.Repeat("C", 40)
	repo := t.TempDir()
	mkGitRepo(t, repo, sha+"\n")
	info, _ := ReadGitInfo(repo)
	if info.Branch != "" || info.Commit != sha {
		t.Fatalf("info = %#v", info)
	}
}

func TestReadGitInfo_WorktreeGitFile(t *testing.T) {
	sha := strings.Repeat("f", 40)
	base := t.TempDir()
	mainGit := filepath.Join(base, "main", ".git")
	wtGit := filepath.Join(mainGit, "worktrees", "wt")
	mustWrite(t, filepath.Join(wtGit, "HEAD"), "ref: refs/heads/wt-branch\n")
	mustWrite(t, filepath.Join(wtGit, "commondir"), "../..\n")
	mustWrite(t, filepath.Join(mainGit, "refs", "heads", "wt-branch"), sha+"\n")
	mustWrite(t, filepath.Join(base, "wt", ".git"), "gitdir: "+wtGit+"\n")

	info, err := ReadGitInfo(filepath.Join(base, "wt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "wt-branch" || info.Commit != sha {
		t.Fatalf("info = %#v", info)
	}
}

func TestReadGitInfo_RejectsRefTraversal(t *testing.T) {
	for _, head := range []string{"ref: ../../outside\n", "ref: refs/heads/../../x\n"} {
		repo := t.TempDir()
		mkGitRepo(t, repo, head)
		if _, err := ReadGitInfo(repo); err == nil || !strings.Contains(err.Error(), "invalid ref") {
			t.Fatalf("HEAD %q: err = %v, want invalid ref", head, err)
		}
	}
}

func TestReadGitInfo_InvalidLooseRefYieldsEmptyCommit(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(repo, ".git", "refs", "heads", "main"), "not-a-sha\n")
	info, err := ReadGitInfo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "main" || info.Commit != "" {
		t.Fatalf("info = %#v", info)
	}
}

func TestReadGitInfo_InvalidDetachedHead(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "0123abcd\n")
	if _, err := ReadGitInfo(repo); err == nil {
		t.Fatal("short detached HEAD must be rejected")
	}
}

func TestReadGitInfo_NonRegularHead(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "HEAD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGitInfo(repo); err == nil {
		t.Fatal("directory HEAD must be rejected")
	}
}

func TestStripURLCredentials(t *testing.T) {
	cases := map[string]string{
		"https://bot:p%zz@host/g/r.git": "https://host/g/r.git",
		"git@github.com:g/r.git":        "git@github.com:g/r.git",
		"https://tok@host/r":            "https://host/r",
	}
	for in, want := range cases {
		if got := stripURLCredentials(in); got != want {
			t.Fatalf("stripURLCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}
