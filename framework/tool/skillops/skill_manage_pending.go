package toolskill

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sixath/framework/skills"
)

const skillManagePreviewMaxLen = 500

// Default tombstone TTL (seconds) for superseded / already_used reasons.
const skillManageTombstoneTTLSeconds = 300

// PendingSkillManage holds a write intent until user confirms.
type PendingSkillManage struct {
	Token       string    `json:"token"`
	Action      string    `json:"action"`
	Name        string    `json:"name"`
	Content     string    `json:"content,omitempty"`
	OldString   string    `json:"old_string,omitempty"`
	NewString   string    `json:"new_string,omitempty"`
	ReplaceAll  bool      `json:"replace_all,omitempty"`
	FilePath    string    `json:"file_path,omitempty"`
	FileContent string    `json:"file_content,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ToolCallID  string    `json:"tool_call_id,omitempty"`
}

// SkillManagePendingStore stores session-scoped pending skill_manage operations.
type SkillManagePendingStore interface {
	SavePending(ctx context.Context, sessionID string, p PendingSkillManage) error
	GetPending(ctx context.Context, sessionID, token string) (*PendingSkillManage, error)
	DeletePending(ctx context.Context, sessionID, token string) error
	ConsumePending(ctx context.Context, sessionID, token string) error
	TombstoneReason(ctx context.Context, sessionID, token string) (string, bool)
}

// SkillManagePendingResponse is returned on propose (before confirm).
type SkillManagePendingResponse struct {
	Status    string                `json:"status"`
	Token     string                `json:"token"`
	Action    string                `json:"action"`
	Name      string                `json:"name"`
	Preview   string                `json:"preview"`
	ExpiresIn int                   `json:"expires_in"`
	Warnings  []skills.SkillWarning `json:"warnings,omitempty"`
}

type skillManageTombstone struct {
	Reason string
	At     time.Time
}

// InMemorySkillManagePendingStore is a process-local pending store.
type InMemorySkillManagePendingStore struct {
	mu         sync.RWMutex
	items      map[string]PendingSkillManage
	tombstones map[string]skillManageTombstone
	tombTTL    time.Duration
}

func NewInMemorySkillManagePendingStore() *InMemorySkillManagePendingStore {
	return &InMemorySkillManagePendingStore{
		items:      make(map[string]PendingSkillManage),
		tombstones: make(map[string]skillManageTombstone),
		tombTTL:    time.Duration(skillManageTombstoneTTLSeconds) * time.Second,
	}
}

func (s *InMemorySkillManagePendingStore) key(sessionID, token string) string {
	return sessionID + ":" + token
}

func (s *InMemorySkillManagePendingStore) writeTombstoneLocked(k, reason string) {
	s.tombstones[k] = skillManageTombstone{Reason: reason, At: time.Now()}
}

func (s *InMemorySkillManagePendingStore) SavePending(_ context.Context, sessionID string, p PendingSkillManage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 同 session + action + name 的新 pending 使旧 token 失效，避免用户确认到已被替代的提案。
	if p.Name != "" && p.Action != "" {
		for k, e := range s.items {
			if strings.HasPrefix(k, sessionID+":") && e.Name == p.Name && e.Action == p.Action && e.Token != p.Token {
				delete(s.items, k)
				s.writeTombstoneLocked(k, "superseded")
			}
		}
	}
	s.items[s.key(sessionID, p.Token)] = p
	return nil
}

func (s *InMemorySkillManagePendingStore) GetPending(_ context.Context, sessionID, token string) (*PendingSkillManage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.items[s.key(sessionID, token)]; ok {
		cp := p
		return &cp, nil
	}
	return nil, nil
}

func (s *InMemorySkillManagePendingStore) DeletePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, s.key(sessionID, token))
	return nil
}

func (s *InMemorySkillManagePendingStore) ConsumePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(sessionID, token)
	delete(s.items, k)
	s.writeTombstoneLocked(k, "already_used")
	return nil
}

func (s *InMemorySkillManagePendingStore) TombstoneReason(_ context.Context, sessionID, token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ts, ok := s.tombstones[s.key(sessionID, token)]
	if !ok {
		return "", false
	}
	ttl := s.tombTTL
	if ttl <= 0 {
		ttl = time.Duration(skillManageTombstoneTTLSeconds) * time.Second
	}
	if time.Since(ts.At) > ttl {
		return "", false
	}
	return ts.Reason, true
}

func skillManageRequiresConfirm(action string, requireCreateDelete, requirePatch bool) bool {
	switch action {
	case "create", "delete":
		return requireCreateDelete
	case "patch", "edit", "write_file", "remove_file":
		return requirePatch
	default:
		return false
	}
}

func skillManagePreview(action, name, content string, pending PendingSkillManage) string {
	switch action {
	case "delete":
		return "Delete skill: " + name
	case "create", "edit":
		if len(content) <= skillManagePreviewMaxLen {
			return content
		}
		return content[:skillManagePreviewMaxLen] + "..."
	case "patch":
		preview := "Patch " + name + ":\n- " + pending.OldString + "\n+ " + pending.NewString
		if len(preview) <= skillManagePreviewMaxLen {
			return preview
		}
		return preview[:skillManagePreviewMaxLen] + "..."
	case "write_file":
		preview := "Write " + name + "/" + pending.FilePath + ":\n" + pending.FileContent
		if len(preview) <= skillManagePreviewMaxLen {
			return preview
		}
		return preview[:skillManagePreviewMaxLen] + "..."
	case "remove_file":
		return "Remove " + name + "/" + pending.FilePath
	default:
		return action + " " + name
	}
}

func pendingParamsFromSkillManage(p PendingSkillManage) map[string]any {
	params := map[string]any{
		"action": p.Action,
		"name":   p.Name,
	}
	if p.Content != "" {
		params["content"] = p.Content
	}
	if p.OldString != "" || p.Action == "patch" {
		params["old_string"] = p.OldString
		params["new_string"] = p.NewString
		params["replace_all"] = p.ReplaceAll
	}
	if p.FilePath != "" {
		params["file_path"] = p.FilePath
	}
	if p.FileContent != "" {
		params["file_content"] = p.FileContent
	}
	return params
}
