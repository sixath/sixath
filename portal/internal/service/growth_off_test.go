package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGrowthWorkerGoRemoved(t *testing.T) {
	_, err := os.Stat("growth_worker.go")
	if err == nil {
		t.Fatal("growth_worker.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestCuratorWorkerGoRemoved(t *testing.T) {
	_, err := os.Stat("curator_worker.go")
	if err == nil {
		t.Fatal("curator_worker.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestMainGo_doesNotProvideGrowthWorker(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	mainPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "backend", "main.go"))
	b, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"provideGrowthWorker", "NewGrowthWorker", "NewCuratorWorker", "GrowthWorker"} {
		if strings.Contains(src, needle) {
			t.Errorf("main.go must not contain %q", needle)
		}
	}
}
