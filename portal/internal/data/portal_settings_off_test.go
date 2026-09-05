package data

import (
	"os"
	"testing"
)

func TestPortalSettingsGoRemoved(t *testing.T) {
	if _, err := os.Stat("portal_settings.go"); err == nil {
		t.Fatal("portal_settings.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
