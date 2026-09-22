package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const weComAPIBase = "https://qyapi.weixin.qq.com"

// ErrWeComNoUserID is returned when getuserinfo has no userid (e.g. external contact only).
var ErrWeComNoUserID = errors.New("wecom: userid missing")

// WeComOAuthClient exchanges OAuth codes for WeCom user ids.
type WeComOAuthClient interface {
	GetUserID(ctx context.Context, code string) (userid string, err error)
}

type weComOAuthClient struct {
	corpID, secret string
	httpClient     *http.Client
	baseURL        string

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewWeComOAuthClient creates a client for WeCom gettoken + auth/getuserinfo.
func NewWeComOAuthClient(corpID, secret string, httpClient *http.Client) WeComOAuthClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &weComOAuthClient{
		corpID:     corpID,
		secret:     secret,
		httpClient: httpClient,
		baseURL:    weComAPIBase,
	}
}

func newWeComOAuthClientForTest(corpID, secret string, httpClient *http.Client, baseURL string) *weComOAuthClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &weComOAuthClient{
		corpID:     corpID,
		secret:     secret,
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

type weComBaseResp struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type weComTokenResp struct {
	weComBaseResp
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type weComUserInfoResp struct {
	weComBaseResp
	UserID string `json:"userid"`
	OpenID string `json:"openid"`
}

func (c *weComOAuthClient) GetUserID(ctx context.Context, code string) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	return c.fetchUserID(ctx, token, code)
}

func (c *weComOAuthClient) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExp) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	tok, exp, err := c.fetchAccessToken(ctx)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.token = tok
	c.tokenExp = exp
	c.mu.Unlock()
	return tok, nil
}

func (c *weComOAuthClient) fetchAccessToken(ctx context.Context) (string, time.Time, error) {
	u := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		c.baseURL, url.QueryEscape(c.corpID), url.QueryEscape(c.secret))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, err
	}
	var tr weComTokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", time.Time{}, err
	}
	if tr.ErrCode != 0 {
		return "", time.Time{}, fmt.Errorf("wecom gettoken: %s", tr.ErrMsg)
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, errors.New("wecom gettoken: empty access_token")
	}
	exp := time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return tr.AccessToken, exp, nil
}

func (c *weComOAuthClient) fetchUserID(ctx context.Context, token, code string) (string, error) {
	u := fmt.Sprintf("%s/cgi-bin/auth/getuserinfo?access_token=%s&code=%s",
		c.baseURL, url.QueryEscape(token), url.QueryEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var ur weComUserInfoResp
	if err := json.Unmarshal(body, &ur); err != nil {
		return "", err
	}
	if ur.ErrCode != 0 {
		return "", fmt.Errorf("wecom getuserinfo: %s", ur.ErrMsg)
	}
	if ur.UserID == "" {
		return "", ErrWeComNoUserID
	}
	return ur.UserID, nil
}
