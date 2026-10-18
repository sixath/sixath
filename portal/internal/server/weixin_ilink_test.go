package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
)

func TestIlinkPeerToView_HidesContextToken(t *testing.T) {
	at := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	v := ilinkPeerToView(&biz.IlinkPeer{
		PeerID: "p1@im.wechat", Allowed: true, Remark: "张三",
		ContextToken: "secret-ctx", ContextTokenAt: &at, LastInboundAt: &at, LastPreview: "hi",
	})
	if !v.HasContextToken || v.ContextTokenAt != "2026-10-18T02:00:00Z" || v.LastInboundAt != "2026-10-18T02:00:00Z" {
		t.Fatalf("view = %+v", v)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "secret-ctx") {
		t.Fatalf("view leaks context_token: %s", raw)
	}
}

func TestIlinkPeerToView_NoToken(t *testing.T) {
	v := ilinkPeerToView(&biz.IlinkPeer{PeerID: "p1"})
	if v.HasContextToken || v.ContextTokenAt != "" || v.LastInboundAt != "" {
		t.Fatalf("view = %+v", v)
	}
}
