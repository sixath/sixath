package conf

import (
	"os"
	"strconv"
)

// VaultConfig 是经验知识 vault 的环境配置。
type VaultConfig struct {
	Enabled bool
	Root    string
}

// LoadVaultConfig 从环境变量读取 vault 配置。
// SATH_VAULT_ENABLED 非 "true" 一律视为关闭；SATH_VAULT_ROOT 为 vault 根绝对路径。
func LoadVaultConfig() VaultConfig {
	enabled, _ := strconv.ParseBool(os.Getenv("SATH_VAULT_ENABLED"))
	return VaultConfig{
		Enabled: enabled,
		Root:    os.Getenv("SATH_VAULT_ROOT"),
	}
}
