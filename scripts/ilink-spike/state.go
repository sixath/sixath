package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type PeerState struct {
	ContextToken   string    `json:"context_token"`
	ContextTokenAt time.Time `json:"context_token_at"`
	BotIDAtToken   string    `json:"bot_id_at_token"`
	LastText       string    `json:"last_text"`
}

type State struct {
	BaseURL     string                `json:"base_url"`
	BotToken    string                `json:"bot_token"`
	BotID       string                `json:"bot_id"`
	OwnerUserID string                `json:"owner_user_id"`
	LoginCount  int                   `json:"login_count"`
	SavedAt     time.Time             `json:"saved_at"`
	Cursor      string                `json:"cursor"`
	Peers       map[string]*PeerState `json:"peers"`
}

func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Peers: map[string]*PeerState{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if st.Peers == nil {
		st.Peers = map[string]*PeerState{}
	}
	return &st, nil
}

func (s *State) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *State) recordInbound(m Message, now time.Time) {
	if m.MessageType != messageTypeUser || m.FromUserID == "" {
		return
	}
	p := s.Peers[m.FromUserID]
	if p == nil {
		p = &PeerState{}
		s.Peers[m.FromUserID] = p
	}
	if m.ContextToken != "" {
		p.ContextToken = m.ContextToken
		p.ContextTokenAt = now
		p.BotIDAtToken = s.BotID
	}
	if text := m.Text(); text != "" {
		p.LastText = truncate(text, 40)
	}
}

// buildLengthText 生成恰好 n 个字符的文本：头部标明长度，每第 100 个字符为 '|'，以【END】结尾，
// 便于在微信里判断是否被截断或拆分。zh=true 时用中文填充（每字 3 字节），用于探测按字节计的上限。
func buildLengthText(n int, zh bool) string {
	fill := func(pos int) rune {
		switch {
		case pos%100 == 0:
			return '|'
		case zh:
			return '测'
		default:
			return rune('0' + pos%10)
		}
	}
	head := []rune(fmt.Sprintf("【长度测试 %d】", n))
	tail := []rune("【END】")
	out := make([]rune, 0, n)
	if n < len(head)+len(tail) {
		for len(out) < n {
			out = append(out, fill(len(out)+1))
		}
		return string(out)
	}
	out = append(out, head...)
	for len(out) < n-len(tail) {
		out = append(out, fill(len(out)+1))
	}
	out = append(out, tail...)
	return string(out)
}
