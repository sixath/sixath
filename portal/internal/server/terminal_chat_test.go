package server

import "testing"

func TestGuessCmdFromNL_TailLog(t *testing.T) {
	got := guessCmdFromNL("查看 cgvmagent.log的后20行数据")
	want := `powershell -NoProfile -Command "Get-Content -Path 'cgvmagent.log' -Tail 20 -Encoding UTF8"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGuessCmdFromNL_ListDir(t *testing.T) {
	if got := guessCmdFromNL("查看当前目录"); got != "dir" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractCmd_FirstLine(t *testing.T) {
	got := extractCmd("dir\n\nHope this helps!")
	if got != "dir" {
		t.Fatalf("got %q", got)
	}
}
