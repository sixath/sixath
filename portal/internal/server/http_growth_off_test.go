package server

import (
	"os"
	"strings"
	"testing"
)

func TestGrowthMetricsFileRemoved(t *testing.T) {
	_, err := os.Stat("growth_metrics.go")
	if err == nil {
		t.Fatal("growth_metrics.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestHTTP_OmitsGrowthMetricsRoute(t *testing.T) {
	b, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "/growth/metrics") {
		t.Fatal("growth metrics must not be registered on the HTTP shell")
	}
}
