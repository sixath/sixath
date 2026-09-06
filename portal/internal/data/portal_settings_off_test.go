package data

import (
	"os"
	"strings"
	"testing"
)

func TestPortalSettingsGoRemoved(t *testing.T) {
	if _, err := os.Stat("portal_settings.go"); err == nil {
		t.Fatal("portal_settings.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestPortalSettingModelRemoved(t *testing.T) {
	if _, err := os.Stat("model/portal_setting.go"); err == nil {
		t.Fatal("model/portal_setting.go must not exist")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestDataGo_omitsPortalSettingAutoMigrate(t *testing.T) {
	b, err := os.ReadFile("data.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "PortalSetting") {
		t.Fatal("data.go must not AutoMigrate PortalSetting")
	}
}
