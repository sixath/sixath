package conf

import "testing"

func TestVaultConfigDefaultsDisabled(t *testing.T) {
	t.Setenv("SATH_VAULT_ENABLED", "")
	t.Setenv("SATH_VAULT_ROOT", "")
	c := LoadVaultConfig()
	if c.Enabled {
		t.Fatal("vault should be disabled by default")
	}
	if c.Root != "" {
		t.Fatal("root should be empty by default")
	}
}

func TestVaultConfigEnabled(t *testing.T) {
	t.Setenv("SATH_VAULT_ENABLED", "true")
	t.Setenv("SATH_VAULT_ROOT", "E:/sitath/knowledge")
	c := LoadVaultConfig()
	if !c.Enabled {
		t.Fatal("vault should be enabled")
	}
	if c.Root != "E:/sitath/knowledge" {
		t.Fatalf("bad root: %s", c.Root)
	}
}
