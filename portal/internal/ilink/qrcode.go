package ilink

import (
	"encoding/base64"
	"errors"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// QRCodePNGDataURL renders qrcode_img_content (a URL) as a PNG data URL for <img src>.
func QRCodePNGDataURL(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errors.New("ilink: empty qrcode content")
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
