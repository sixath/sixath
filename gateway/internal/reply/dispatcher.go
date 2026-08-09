package reply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// FinalPayload is posted to webhook reply_url (and returned for sync mode).
type FinalPayload struct {
	CorrelationID string `json:"correlation_id"`
	Status        string `json:"status"`
	Content       string `json:"content,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Dispatcher delivers final replies (webhook reply_url POST; SSE in later tasks).
type Dispatcher struct {
	httpClient *http.Client
}

// NewDispatcher builds a ReplyDispatcher. httpClient may be nil for defaults.
func NewDispatcher(httpClient *http.Client) *Dispatcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Dispatcher{httpClient: httpClient}
}

// PostReplyURL POSTs the final payload to replyURL.
// Empty replyURL is a no-op (logged).
func (d *Dispatcher) PostReplyURL(ctx context.Context, replyURL string, payload FinalPayload) error {
	if replyURL == "" {
		log.Printf("reply: no reply_url; correlation_id=%s status=%s", payload.CorrelationID, payload.Status)
		return nil
	}
	if err := ValidateReplyURL(replyURL); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode reply payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, replyURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("reply_url status %d", resp.StatusCode)
	}
	return nil
}
