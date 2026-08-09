package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultQYAPIBase = "https://qyapi.weixin.qq.com"

// Directory resolves WeCom userid / open_userid to a display name via the contacts API.
// Requires a self-built app access_token (corp_id + corp_secret) with member read permission.
type Directory struct {
	corpID  string
	secret  string
	baseURL string
	http    *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
	names    map[string]string // cache key: raw asker id from callback
}

// DirectoryConfig configures a Directory client.
type DirectoryConfig struct {
	CorpID  string
	Secret  string
	BaseURL string // optional; defaults to qyapi.weixin.qq.com (override in tests)
	HTTP    *http.Client
}

// NewDirectory returns nil when corp credentials are incomplete.
func NewDirectory(cfg DirectoryConfig) *Directory {
	corpID := strings.TrimSpace(cfg.CorpID)
	secret := strings.TrimSpace(cfg.Secret)
	if corpID == "" || secret == "" {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = defaultQYAPIBase
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 8 * time.Second}
	}
	return &Directory{
		corpID:  corpID,
		secret:  secret,
		baseURL: base,
		http:    hc,
		names:   make(map[string]string),
	}
}

// ResolveDisplayName returns a human-readable name for askerID.
// On any API failure it returns askerID unchanged (never blocks the reply path with an error).
func (d *Directory) ResolveDisplayName(ctx context.Context, askerID string) string {
	askerID = strings.TrimSpace(askerID)
	if d == nil || askerID == "" {
		return askerID
	}
	if name, ok := d.cachedName(askerID); ok {
		return name
	}

	token, err := d.accessToken(ctx)
	if err != nil {
		return askerID
	}

	name, err := d.userGetName(ctx, token, askerID)
	if err != nil {
		plain, convErr := d.openUserIDToUserID(ctx, token, askerID)
		if convErr != nil || plain == "" || plain == askerID {
			return askerID
		}
		name, err = d.userGetName(ctx, token, plain)
		if err != nil || name == "" {
			return askerID
		}
	}
	if name == "" {
		return askerID
	}
	d.storeName(askerID, name)
	return name
}

func (d *Directory) cachedName(askerID string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	name, ok := d.names[askerID]
	return name, ok
}

func (d *Directory) storeName(askerID, name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.names[askerID] = name
}

func (d *Directory) accessToken(ctx context.Context) (string, error) {
	d.mu.Lock()
	if d.token != "" && time.Now().Before(d.tokenExp) {
		tok := d.token
		d.mu.Unlock()
		return tok, nil
	}
	d.mu.Unlock()

	u := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		d.baseURL, url.QueryEscape(d.corpID), url.QueryEscape(d.secret))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		return "", fmt.Errorf("gettoken: %d %s", out.ErrCode, out.ErrMsg)
	}
	exp := out.ExpiresIn
	if exp <= 0 {
		exp = 7200
	}
	// Refresh slightly before expiry.
	ttl := time.Duration(exp)*time.Second - 5*time.Minute
	if ttl < time.Minute {
		ttl = time.Minute
	}

	d.mu.Lock()
	d.token = out.AccessToken
	d.tokenExp = time.Now().Add(ttl)
	tok := d.token
	d.mu.Unlock()
	return tok, nil
}

func (d *Directory) userGetName(ctx context.Context, token, userID string) (string, error) {
	u := fmt.Sprintf("%s/cgi-bin/user/get?access_token=%s&userid=%s",
		d.baseURL, url.QueryEscape(token), url.QueryEscape(userID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Name    string `json:"name"`
		Alias   string `json:"alias"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 {
		return "", fmt.Errorf("user/get: %d %s", out.ErrCode, out.ErrMsg)
	}
	return formatWeComDisplayName(out.Name, out.Alias), nil
}

func (d *Directory) openUserIDToUserID(ctx context.Context, token, openUserID string) (string, error) {
	u := fmt.Sprintf("%s/cgi-bin/batch/openuserid_to_userid?access_token=%s",
		d.baseURL, url.QueryEscape(token))
	payload, _ := json.Marshal(map[string]any{
		"open_userid_list": []string{openUserID},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		ErrCode    int    `json:"errcode"`
		ErrMsg     string `json:"errmsg"`
		UserIDList []struct {
			OpenUserID string `json:"open_userid"`
			UserID     string `json:"userid"`
		} `json:"userid_list"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ErrCode != 0 {
		return "", fmt.Errorf("openuserid_to_userid: %d %s", out.ErrCode, out.ErrMsg)
	}
	for _, row := range out.UserIDList {
		if row.OpenUserID == openUserID && strings.TrimSpace(row.UserID) != "" {
			return row.UserID, nil
		}
	}
	return "", fmt.Errorf("openuserid_to_userid: no mapping for %q", openUserID)
}

// formatWeComDisplayName matches common WeCom UI: alias(name) when both differ.
func formatWeComDisplayName(name, alias string) string {
	name = strings.TrimSpace(name)
	alias = strings.TrimSpace(alias)
	switch {
	case name == "" && alias == "":
		return ""
	case alias != "" && name != "" && alias != name:
		return alias + "(" + name + ")"
	case name != "":
		return name
	default:
		return alias
	}
}
