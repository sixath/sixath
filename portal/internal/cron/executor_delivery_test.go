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

	if err := e.deliverWebhook(context.Background(), task, "run1", "success", "## 天气\n晴", nil, now, now); err != nil {
		t.Fatalf("delivery to a WeCom robot should succeed: %v", err)
	}
	md, _ := got["markdown"].(map[string]any)
	if got["msgtype"] != "markdown" || md["content"] != "## 天气\n晴" {
		t.Fatalf("WeCom payload = %v, want the output as markdown", got)
	}

	if err := e.deliverWebhook(context.Background(), task, "run2", "failed", "", errors.New("deadline exceeded"), now, now); err != nil {
		t.Fatalf("failure notice should be delivered: %v", err)
	}
	md, _ = got["markdown"].(map[string]any)
	if c, _ := md["content"].(string); !strings.Contains(c, "中午推荐") || !strings.Contains(c, "执行失败") || !strings.Contains(c, "deadline exceeded") {
		t.Fatalf("failure content = %q", c)
	}

	if err := e.deliverWebhook(context.Background(), task, "run3", "success", "", nil, now, now); err != nil {
		t.Fatalf("empty output should still be delivered: %v", err)
	}
	md, _ = got["markdown"].(map[string]any)
	if c, _ := md["content"].(string); !strings.Contains(c, "无输出") {
		t.Fatalf("empty output content = %q, want a placeholder", c)
	}

	reply = `{"errcode":93000,"errmsg":"invalid webhook url"}`
	err := e.deliverWebhook(context.Background(), task, "run4", "success", "x", nil, now, now)
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Fatalf("a WeCom errcode must fail the delivery with the code, got %v", err)
	}
}

func TestDeliverWebhook_ErrorHidesKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	e := newTestExecutor()
	now := time.Now()
	for _, path := range []string{"/cgi-bin/webhook/send?key=secret-key-1", "/hook?token=secret-key-1"} {
		task := &biz.CronTaskMeta{ID: "t1", Name: "n", DeliveryWebhookURL: srv.URL + path}
		err := e.deliverWebhook(context.Background(), task, "run1", "success", "x", nil, now, now)
		if err == nil {
			t.Fatalf("%s: delivery to a closed server must fail", path)
		}
		if strings.Contains(err.Error(), "secret-key-1") {
			t.Fatalf("%s: delivery error leaks the key: %v", path, err)
		}
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

	if err := e.deliverWebhook(context.Background(), task, "run1", "success", long, nil, now, now); err != nil {
		t.Fatalf("generic delivery should succeed: %v", err)
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
