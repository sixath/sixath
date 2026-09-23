package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	cdRe    = regexp.MustCompile(`(?i)^\s*cd(?:\s+(.*))?$`)
	driveRe = regexp.MustCompile(`(?i)^\s*([a-z]:)(\\.*)?\s*$`)
)

type Session struct {
	ID        string
	VMID      int64
	Host      string
	Port      int
	WorkDir   string
	CreatedAt time.Time
	LastUsed  time.Time
	mu        sync.Mutex
}

type Manager struct {
	mu           sync.Mutex
	sessions     map[string]*Session
	db           *gorm.DB
	client       *http.Client
	idleTTL      time.Duration
	lookupIPFunc func(ctx context.Context, vmid int64) (string, error)
}

func NewManager(db *gorm.DB, idleTTL time.Duration) *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
		db:       db,
		client:   &http.Client{Timeout: 30 * time.Second},
		idleTTL:  idleTTL,
	}
	go m.cleanupLoop()
	return m
}

// SetVMIPLookup sets an optional IP lookup function (e.g. backed by agent datasource tools).
// When set, it replaces the default DB query in lookupIP.
func (m *Manager) SetVMIPLookup(fn func(ctx context.Context, vmid int64) (string, error)) {
	m.lookupIPFunc = fn
}

// NewDBLookup creates a VMIPLookup that queries t_game_virtual_machine_info via gorm.
func NewDBLookup(db *gorm.DB) func(ctx context.Context, vmid int64) (string, error) {
	return func(ctx context.Context, vmid int64) (string, error) {
		var ip string
		err := db.WithContext(ctx).Raw(
			"SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?", vmid,
		).Scan(&ip).Error
		if err != nil {
			return "", fmt.Errorf("VM IP lookup failed: %w", err)
		}
		if ip == "" {
			return "", fmt.Errorf("VM_NOT_FOUND: no IP found for vmid %d", vmid)
		}
		return ip, nil
	}
}

// CreateWithHost creates a session with a pre-resolved host, skipping IP lookup.
func (m *Manager) CreateWithHost(VMID int64, host string, port int, workdir string) (*Session, error) {
	if VMID <= 0 {
		return nil, fmt.Errorf("vmid must be positive")
	}
	if host == "" {
		return nil, fmt.Errorf("host must not be empty")
	}
	if port < 1 || port > 65535 {
		port = 53000
	}
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		workdir = "C:\\"
	}

	s := &Session{
		ID:        uuid.NewString(),
		VMID:      VMID,
		Host:      host,
		Port:      port,
		WorkDir:   workdir,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) Create(VMID int64, port int, workdir string) (*Session, error) {
	if VMID <= 0 {
		return nil, fmt.Errorf("vmid must be positive")
	}
	if port < 1 || port > 65535 {
		port = 53000
	}
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		workdir = "C:\\"
	}

	host, err := m.lookupIP(VMID)
	if err != nil {
		return nil, err
	}

	s := &Session{
		ID:        uuid.NewString(),
		VMID:      VMID,
		Host:      host,
		Port:      port,
		WorkDir:   workdir,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	return ok
}

func (m *Manager) Execute(id, input string) (string, error) {
	s := m.Get(id)
	if s == nil {
		return "", fmt.Errorf("session not found")
	}
	return s.execute(m.client, input)
}

func (m *Manager) lookupIP(vmid int64) (string, error) {
	if m.lookupIPFunc != nil {
		return m.lookupIPFunc(context.Background(), vmid)
	}
	if m.db == nil {
		return "", fmt.Errorf("database not available for VM IP lookup")
	}
	var ip string
	err := m.db.Raw("SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?", vmid).Scan(&ip).Error
	if err != nil {
		return "", fmt.Errorf("VM IP lookup failed: %w", err)
	}
	if ip == "" {
		return "", fmt.Errorf("VM_NOT_FOUND: no IP found for vmid %d", vmid)
	}
	return ip, nil
}

func (s *Session) execute(client *http.Client, input string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd, newDir := buildCommand(s.WorkDir, input)
	s.WorkDir = newDir
	s.LastUsed = time.Now()

	body := map[string]string{"cmd": cmd}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("http://%s:%d/runCmd", s.Host, s.Port)
	resp, err := client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return formatRunCmdOutput(string(out)), nil
}

// runCmdResponse is the JSON shape returned by the VM agent /runCmd endpoint.
type runCmdResponse struct {
	CodeDesc string `json:"codeDesc"`
	RetCode  int    `json:"retCode"`
}

// formatRunCmdOutput unwraps {"codeDesc","retCode"} into plain text for terminal display.
// Non-JSON payloads are returned unchanged. Empty successful output becomes "".
func formatRunCmdOutput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var resp runCmdResponse
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		return raw
	}
	out := resp.CodeDesc
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.Trim(out, "\n")
	if resp.RetCode != 0 {
		if out == "" {
			return fmt.Sprintf("[exit %d]", resp.RetCode)
		}
		return fmt.Sprintf("%s\n[exit %d]", out, resp.RetCode)
	}
	return out
}

func parseCDCommand(input string) (dir string, isCD bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	// Bare drive change: "D:" / "D:\" / "D:\logs"
	if m := driveRe.FindStringSubmatch(input); m != nil {
		dir = strings.ToUpper(m[1][:1]) + ":"
		if m[2] != "" {
			dir += m[2]
		} else {
			dir += "\\"
		}
		return normalizeWorkDir(dir), true
	}
	m := cdRe.FindStringSubmatch(input)
	if m == nil {
		return "", false
	}
	dir = strings.TrimSpace(m[1])
	dir = strings.TrimPrefix(dir, "/d ")
	dir = strings.TrimPrefix(dir, "/D ")
	dir = strings.TrimSpace(dir)
	dir = strings.Trim(dir, `"'`)
	return dir, true
}

func normalizeWorkDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "C:\\"
	}
	dir = strings.ReplaceAll(dir, "/", "\\")
	if len(dir) == 2 && dir[1] == ':' {
		return strings.ToUpper(dir[:1]) + ":\\"
	}
	if len(dir) >= 2 && dir[1] == ':' {
		return strings.ToUpper(dir[:1]) + dir[1:]
	}
	return dir
}

func buildCommand(workDir, input string) (cmd, newDir string) {
	workDir = normalizeWorkDir(workDir)
	cdDir, isCD := parseCDCommand(input)
	if isCD {
		if cdDir == "" {
			newDir = workDir
		} else if len(cdDir) >= 2 && cdDir[1] == ':' {
			newDir = normalizeWorkDir(cdDir)
		} else {
			newDir = normalizeWorkDir(strings.TrimRight(workDir, "\\") + "\\" + cdDir)
		}
		return "cd /d " + newDir, newDir
	}
	return "cd /d " + workDir + " && " + input, workDir
}

func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-m.idleTTL)
	for id, s := range m.sessions {
		if !s.LastUsed.After(cutoff) {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.cleanupExpired()
	}
}