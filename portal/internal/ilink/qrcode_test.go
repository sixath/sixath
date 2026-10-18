package ilink

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestQRCodePNGDataURL(t *testing.T) {
	got, err := QRCodePNGDataURL("https://liteapp.weixin.qq.com/q/7GiQu1?qrcode=abc&bot_type=3")
	if err != nil {
		t.Fatalf("QRCodePNGDataURL: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("prefix = %q", got[:30])
	}
	png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, prefix))
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	if !strings.HasPrefix(string(png), "\x89PNG") {
		t.Fatal("not a PNG")
	}
}

func TestQRCodePNGDataURLEmpty(t *testing.T) {
	if _, err := QRCodePNGDataURL("  "); err == nil {
		t.Fatal("want error for empty content")
	}
}
