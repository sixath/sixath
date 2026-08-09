package tool

import (
	"crypto/rand"
	"encoding/hex"
)

// TokenGenerator 用于生成确认 token（写/改确认、ask_user、skill 管理待确认等共用）。
type TokenGenerator interface {
	NewToken() (string, error)
}

// RandomTokenGenerator 使用 crypto/rand 生成十六进制随机 token。
type RandomTokenGenerator struct {
	// BytesLen 为随机字节长度，若为 0 则默认 16 字节（32 字符十六进制）。
	BytesLen int
}

func (g RandomTokenGenerator) NewToken() (string, error) {
	n := g.BytesLen
	if n <= 0 {
		n = 16
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
