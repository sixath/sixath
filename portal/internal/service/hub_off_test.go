package service

import (
	"os"
	"testing"
)

func TestHubWireFileRemoved(t *testing.T) {
	_, err := os.Stat("hub_wire.go")
	if err == nil {
		t.Fatal("hub_wire.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
