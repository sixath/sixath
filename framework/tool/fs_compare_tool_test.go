package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

const fsWinSubject = ` 驱动器 G 中的卷是 Data

 G:\yysls\LocalData\Patch 的目录

2026/09/25  04:48                 0 repair
2026/09/25  03:28     1,234,567,890 Patch1.mpk
2026/09/25  03:27            12,345 patching_version.txt
2026/09/20  10:00    <DIR>          logs
               3 个文件  1,234,580,235 字节
`

const fsWinHealthy = `
 G:\yysls\LocalData\Patch 的目录

2026/09/28  12:00     1,234,567,890 Patch1.mpk
2026/09/28  12:00            12,345 patching_version.txt
2026/09/20  10:00    <DIR>          logs
`

func fsFakeRegistry(t *testing.T, outputs map[string]string) *Registry {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Register(Tool{
		Name: "vm_run_cmd", Effect: EffectRead,
		Execute: func(ctx context.Context, p map[string]any) (any, error) {
			if p["op"] != RemoteOpListRecent || p["path"] == nil {
				t.Errorf("unexpected args %v", p)
			}
			ip, _ := p["ip"].(string)
			out, ok := outputs[ip]
			if !ok {
				return map[string]any{"ok": false, "error": "timed out"}, nil
			}
			return map[string]any{"ok": true, "stdout": out}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterFsCompareTool(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestFsCompare_WindowsListings(t *testing.T) {
	reg := fsFakeRegistry(t, map[string]string{"bad": fsWinSubject, "good": fsWinHealthy})
	tl, _ := reg.Get(FsCompareToolName)
	res, _ := tl.Execute(context.Background(), map[string]any{
		"path":  `G:\yysls\LocalData\Patch`,
		"onset": "2026-09-25 08:10",
		"window": "6h",
		"targets": []any{
			map[string]any{"label": "vm-255266", "args": map[string]any{"ip": "bad"}},
			map[string]any{"label": "healthy", "args": map[string]any{"ip": "good"}},
		},
	})
	out := res.(map[string]any)
	if out["ok"] != true {
		t.Fatalf("out=%v", out)
	}
	only := out["only_in"].(map[string]any)
	subj := only["vm-255266"].([]string)
	if len(subj) != 1 || !strings.HasPrefix(subj[0], "repair (2026-09-25 04:48") {
		t.Fatalf("only_in subject = %v", subj)
	}
	frozen := out["frozen_on_subject"].([]map[string]any)
	if len(frozen) != 2 {
		t.Fatalf("frozen = %v", frozen)
	}
	near := out["modified_near_onset"].([]map[string]any)
	if len(near) != 3 || near[len(near)-1]["path"] != "repair" || near[len(near)-1]["subject_only"] != true {
		t.Fatalf("near = %v", near)
	}
}

func TestFsCompare_FailedHealthyIsNotAbsence(t *testing.T) {
	reg := fsFakeRegistry(t, map[string]string{"bad": fsWinSubject})
	tl, _ := reg.Get(FsCompareToolName)
	res, _ := tl.Execute(context.Background(), map[string]any{
		"path": `G:\x`,
		"targets": []any{
			map[string]any{"label": "bad", "args": map[string]any{"ip": "bad"}},
			map[string]any{"label": "good", "args": map[string]any{"ip": "down"}},
		},
	})
	out := res.(map[string]any)
	if out["only_in"] != nil || !strings.Contains(out["note"].(string), "NOT evidence") {
		t.Fatalf("out=%v", out)
	}
}

func TestParseFsListing_PosixRecursive(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	lines := strings.Split(`/opt/app:
total 8
drwxr-xr-x 2 root root 4096 Sep 25 03:27 state
-rw-r--r-- 1 root root  120 Sep 28 11:00 app.conf

/opt/app/state:
total 4
-rw-r--r-- 1 root root    0 Sep 25 04:48 repair
lrwxrwxrwx 1 root root    9 Dec 31  2025 current -> v1`, "\n")
	got, n := parseFsListing(lines, now)
	if n != 4 {
		t.Fatalf("parsed %d: %+v", n, got)
	}
	r, ok := got["state/repair"]
	if !ok || r.MTime.Format("2006-01-02 15:04") != "2026-09-25 04:48" {
		t.Fatalf("state/repair = %+v", r)
	}
	if !got["state"].Dir || got["state/current"].MTime.Year() != 2025 {
		t.Fatalf("entries = %+v", got)
	}
}

func TestParseFsListing_EnglishDir(t *testing.T) {
	lines := strings.Split(` Directory of C:\app

09/25/2026  04:48 AM                 0 repair
09/25/2026  03:27 PM    <DIR>          cache`, "\n")
	got, _ := parseFsListing(lines, time.Now())
	if got["repair"].MTime.Hour() != 4 || got["cache"].MTime.Hour() != 15 || !got["cache"].Dir {
		t.Fatalf("got %+v", got)
	}
}
