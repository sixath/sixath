package tool

import (
	"strings"
	"testing"
)

const c304RegisterSrc = `package union

func RegisterUnionUserToArea() {
	result, errcode, err := RequestRegisterAreaUser()
	if err != nil {
		vlog.Errorf("request register area user failed")
		return
	}
	info := new(DBUnionUserAreaInfo)
	info.UID, _ = strconv.ParseUint(result.UserID, 10, 64)
	info.State = 1
	if errcode == 0 {
		errcode, err = InsertUnionUserAreaInfo(info)
	}
}
`

func TestExtractControlFlow_c304InsertOnlyOnErrcodeZero(t *testing.T) {
	got := ExtractControlFlow([]byte(c304RegisterSrc), "handler/helper.go", 1, 20)
	if len(got) != 1 {
		t.Fatalf("funcs=%d want 1: %#v", len(got), got)
	}
	fn := got[0]
	if fn.Function != "RegisterUnionUserToArea" {
		t.Fatalf("function=%q", fn.Function)
	}
	var insertPaths, otherPaths []ControlFlowPath
	for _, p := range fn.Paths {
		if pathHasCall(p, "InsertUnionUserAreaInfo") {
			insertPaths = append(insertPaths, p)
		} else {
			otherPaths = append(otherPaths, p)
		}
	}
	if len(insertPaths) == 0 {
		t.Fatalf("InsertUnionUserAreaInfo missing from paths: %#v", fn.Paths)
	}
	for _, p := range insertPaths {
		if !pathHasWhen(p, "errcode == 0") {
			t.Fatalf("Insert path must include errcode == 0, got %#v", p)
		}
	}
	for _, p := range otherPaths {
		if pathHasWhen(p, "errcode == 0") && pathHasCall(p, "InsertUnionUserAreaInfo") {
			t.Fatalf("non-insert path should not both have errcode==0 and Insert: %#v", p)
		}
	}
	// 1105 / errcode != 0 path must exist and must not call Insert.
	foundNeg := false
	for _, p := range otherPaths {
		if pathHasWhen(p, "errcode != 0") && !pathHasCall(p, "InsertUnionUserAreaInfo") {
			foundNeg = true
			break
		}
	}
	if !foundNeg {
		t.Fatalf("expected implicit errcode != 0 path without Insert, paths=%#v", fn.Paths)
	}
}

func TestExtractControlFlow_narrowWindowStillWholeFunc(t *testing.T) {
	// Lines 14-16 are the Insert block; overlapping function is still the whole func.
	got := ExtractControlFlow([]byte(c304RegisterSrc), "helper.go", 14, 16)
	if len(got) != 1 {
		t.Fatalf("funcs=%d want 1", len(got))
	}
	if got[0].EndLine < 14 || got[0].StartLine > 16 {
		t.Fatalf("function span %d-%d should overlap window 14-16", got[0].StartLine, got[0].EndLine)
	}
	found := false
	for _, p := range got[0].Paths {
		if pathHasCall(p, "InsertUnionUserAreaInfo") && pathHasWhen(p, "errcode == 0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("narrow window must still attach Insert under errcode==0: %#v", got[0].Paths)
	}
}

func TestExtractControlFlow_switchCase(t *testing.T) {
	src := `package p
func F() {
	switch errcode {
	case 0:
		InsertUnionUserAreaInfo()
	case 1105:
		reuseExistingUID()
	}
}
`
	got := ExtractControlFlow([]byte(src), "h.go", 1, 20)
	if len(got) != 1 {
		t.Fatalf("funcs=%d: %#v", len(got), got)
	}
	var insert, reuse bool
	for _, p := range got[0].Paths {
		if pathHasCall(p, "InsertUnionUserAreaInfo") {
			insert = true
			if !pathHasWhen(p, "errcode == 0") {
				t.Fatalf("Insert case when=%v", p.When)
			}
		}
		if pathHasCall(p, "reuseExistingUID") {
			reuse = true
			if !pathHasWhen(p, "errcode == 1105") {
				t.Fatalf("reuse case when=%v", p.When)
			}
		}
	}
	if !insert || !reuse {
		t.Fatalf("missing case paths: %#v", got[0].Paths)
	}
}

func TestExtractControlFlow_forRange(t *testing.T) {
	src := `package p
func F() {
	for _, item := range items {
		SyncUnionUser(item)
	}
}
`
	got := ExtractControlFlow([]byte(src), "h.go", 1, 20)
	if len(got) != 1 {
		t.Fatalf("funcs=%d", len(got))
	}
	var with, without bool
	for _, p := range got[0].Paths {
		has := pathHasCall(p, "SyncUnionUser")
		if has {
			with = true
			if !pathHasWhenPrefix(p, "range ") {
				t.Fatalf("loop path when=%v", p.When)
			}
		} else {
			without = true
		}
	}
	if !with || !without {
		t.Fatalf("want enter+skip range paths, got %#v", got[0].Paths)
	}
}

func TestExtractControlFlow_elseBranch(t *testing.T) {
	src := `package p
func F() {
	if ok {
		FooAAAAAAA()
	} else {
		BarBBBBBBB()
	}
}
`
	got := ExtractControlFlow([]byte(src), "h.go", 1, 20)
	if len(got) != 1 {
		t.Fatal(got)
	}
	var foo, bar bool
	for _, p := range got[0].Paths {
		if pathHasCall(p, "FooAAAAAAA") {
			foo = true
			if !pathHasWhen(p, "ok") {
				t.Fatalf("then when=%v", p.When)
			}
		}
		if pathHasCall(p, "BarBBBBBBB") {
			bar = true
			if !pathHasWhen(p, "!ok") && !pathHasWhen(p, "!(ok)") {
				t.Fatalf("else when=%v", p.When)
			}
		}
	}
	if !foo || !bar {
		t.Fatalf("missing branch calls: %#v", got[0].Paths)
	}
}

func TestExtractControlFlow_earlyReturn(t *testing.T) {
	src := `package p
func F() {
	if !exists {
		return
	}
	WriteUnionMapping()
}
`
	got := ExtractControlFlow([]byte(src), "h.go", 1, 20)
	if len(got) != 1 {
		t.Fatal(got)
	}
	var write, early bool
	for _, p := range got[0].Paths {
		if pathHasCall(p, "WriteUnionMapping") {
			write = true
			if !pathHasWhen(p, "exists") && !pathHasWhen(p, "!(!exists)") {
				t.Fatalf("write path when=%v", p.When)
			}
		}
		if p.Returns && !pathHasCall(p, "WriteUnionMapping") {
			early = true
		}
	}
	if !write || !early {
		t.Fatalf("want early-return + write paths, got %#v", got[0].Paths)
	}
}

func TestExtractControlFlow_nonGoNil(t *testing.T) {
	if got := ExtractControlFlow([]byte("hello"), "notes.txt", 1, 10); got != nil {
		t.Fatalf("non-go: %#v", got)
	}
}

func TestExtractControlFlow_parseFailNil(t *testing.T) {
	if got := ExtractControlFlow([]byte("not go {"), "x.go", 1, 10); got != nil {
		t.Fatalf("parse fail: %#v", got)
	}
}

func pathHasCall(p ControlFlowPath, name string) bool {
	for _, c := range p.Calls {
		if c == name {
			return true
		}
	}
	return false
}

func pathHasWhen(p ControlFlowPath, want string) bool {
	want = compactCFGExpr(want)
	for _, w := range p.When {
		if compactCFGExpr(w) == want {
			return true
		}
	}
	return false
}

func pathHasWhenPrefix(p ControlFlowPath, prefix string) bool {
	for _, w := range p.When {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}
