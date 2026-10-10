package cron

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

func newTestExecutor() *Executor {
	return &Executor{log: log.NewHelper(log.DefaultLogger)}
}

func TestDeliverWebhook_WeComRobot(t *testing.T) {
	var (
		got   map[string]any
		reply = `{"errcode":0,"errmsg":"ok"}`
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cgi-bin/webhook/send" || r.URL.Query().Get("key") != "k1" {
			t.Errorf("unexpected request %s", r.URL)
		}
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	e := newTestExecutor()
	task := &biz.CronTaskMeta{ID: "t1", Name: "中午推荐", DeliveryWebhookURL: srv.URL + "/cgi-bin/webhook/send?key=k1"}
	now := time.Now()

	if !e.deliverWebhook(context.Background(), task, "run1", "success", "## 天气\n晴", nil, now, now) {
		t.Fatal("delivery to a WeCom robot should succeed")
	}
	md, _ := got["markdown"].(map[string]any)
	if got["msgtype"] != "markdown" || md["content"] != "## 天气\n晴" {
		t.Fatalf("WeCom payload = %v, want the output as markdown", got)
	}

	if !e.deliverWebhook(context.Background(), task, "run2", "failed", "", errors.New("deadline exceeded"), now, now) {
		t.Fatal("failure notice should be delivered")
	}
	md, _ = got["markdown"].(map[string]any)
	if c, _ := md["content"].(string); !strings.Contains(c, "中午推荐") || !strings.Contains(c, "执行失败") || !strings.Contains(c, "deadline exceeded") {
		t.Fatalf("failure content = %q", c)
	}

	reply = `{"errcode":93000,"errmsg":"invalid webhook url"}`
	if e.deliverWebhook(context.Background(), task, "run3", "success", "x", nil, now, now) {
		t.Fatal("a WeCom errcode must count as a failed delivery")
	}
}

func TestDeliverWebhook_GenericJSON(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	e := newTestExecutor()
	task := &biz.CronTaskMeta{ID: "t1", Name: "n", DeliveryWebhookURL: srv.URL + "/hook"}
	now := time.Now()
	long := strings.Repeat("中", 1000)

	if !e.deliverWebhook(context.Background(), task, "run1", "success", long, nil, now, now) {
		t.Fatal("generic delivery should succeed")
	}
	s, _ := got["output_summary"].(string)
	if got["task_id"] != "t1" || got["run_id"] != "run1" || got["status"] != "success" {
		t.Fatalf("generic payload = %v", got)
	}
	if !utf8.ValidString(s) || len(s) > maxOutputSummaryBytes+len("...") || !strings.HasSuffix(s, "...") {
		t.Fatalf("summary of %d bytes (valid=%v) not truncated on a rune boundary", len(s), utf8.ValidString(s))
	}
}

func TestOutputSummary(t *testing.T) {
	if got := outputSummary("short"); got != "short" {
		t.Fatalf("short output changed: %q", got)
	}
	got := outputSummary(strings.Repeat("中", 1000))
	if !utf8.ValidString(got) || len(got) > maxOutputSummaryBytes+len("...") {
		t.Fatalf("summary of %d bytes, valid=%v", len(got), utf8.ValidString(got))
	}
}
