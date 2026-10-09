package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeHandbookDirs struct {
	dirs []string
	err  error
}

func (f fakeHandbookDirs) SkillDirsForAgent(context.Context, string) ([]string, error) {
	return f.dirs, f.err
}

func TestAppendHandbookDirs(t *testing.T) {
	logger := log.NewHelper(log.DefaultLogger)
	base := []string{"/data/skills/s1"}
	if got := appendHandbookDirs(context.Background(), nil, "ag", base, logger); !reflect.DeepEqual(got, base) {
		t.Fatalf("nil resolver: %v", got)
	}
	got := appendHandbookDirs(context.Background(), fakeHandbookDirs{dirs: []string{"/hb/code-map", "/hb/r1"}}, "ag", base, logger)
	if !reflect.DeepEqual(got, []string{"/data/skills/s1", "/hb/code-map", "/hb/r1"}) {
		t.Fatalf("append: %v", got)
	}
	got = appendHandbookDirs(context.Background(), fakeHandbookDirs{dirs: []string{"/hb/r1"}, err: errors.New("code-map write failed")}, "ag", base, logger)
	if !reflect.DeepEqual(got, []string{"/data/skills/s1", "/hb/r1"}) {
		t.Fatalf("partial result must still be used: %v", got)
	}
	if got := appendHandbookDirs(context.Background(), fakeHandbookDirs{dirs: []string{"/x"}}, "", base, logger); !reflect.DeepEqual(got, base) {
		t.Fatalf("empty agent id: %v", got)
	}
}
