package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSCP_UploadUsesExternalSCP(t *testing.T) {
	dir := t.TempDir()
	localFile := filepath.Join(dir, "storage_worker")
	if err := os.WriteFile(localFile, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeSSHRunner{result: SSHExecRunResult{ExitCode: 0}}
	reg := NewRegistry()
	err := RegisterSCPTool(reg, &SCPConfig{
		SSHExecConfig: SSHExecConfig{
			Runner:                runner,
			DefaultUser:           "root",
			DefaultTimeoutSec:     15,
			StrictHostKeyChecking: "accept-new",
			AllowedHosts:          []string{"10.79.240.149"},
			AllowedUsers:          []string{"root"},
		},
		SCPPath: "scp-custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("scp")
	if !ok {
		t.Fatal("scp not found")
	}

	out, err := tl.Execute(context.Background(), map[string]any{
		"host":         "10.79.240.149",
		"direction":    "upload",
		"local_path":   localFile,
		"remote_path":  "/data/tmp/storage_worker",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res, ok := out.(SCPResult)
	if !ok {
		t.Fatalf("unexpected type %T", out)
	}
	if !res.OK || res.BytesTransferred != 7 || res.ErrorCategory != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if runner.name != "scp-custom" {
		t.Fatalf("runner name = %q", runner.name)
	}
	joined := strings.Join(runner.args, " ")
	for _, want := range []string{
		"-o BatchMode=yes",
		"-o StrictHostKeyChecking=accept-new",
		localFile,
		"root@10.79.240.149:/data/tmp/storage_worker",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("scp args %q missing %q", joined, want)
		}
	}
}

func TestSCP_BlockedByLocalPathPolicy(t *testing.T) {
	dir := t.TempDir()
	localFile := filepath.Join(dir, "secret.bin")
	_ = os.WriteFile(localFile, []byte("x"), 0o644)

	reg := NewRegistry()
	_ = RegisterSCPTool(reg, &SCPConfig{
		SSHExecConfig: SSHExecConfig{
			Runner:       &fakeSSHRunner{},
			DefaultUser:  "root",
			AllowedHosts: []string{"10.0.0.1"},
		},
		AllowedLocalPathPrefixes: []string{filepath.Join(dir, "allowed")},
	})
	tl, _ := reg.Get("scp")
	out, err := tl.Execute(context.Background(), map[string]any{
		"host":        "10.0.0.1",
		"direction":   "upload",
		"local_path":  localFile,
		"remote_path": "/data/tmp/secret.bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	res := out.(SCPResult)
	if res.OK || res.ErrorCategory != SSHExecErrorBlockedByPolicy {
		t.Fatalf("expected blocked, got %+v", res)
	}
}

func TestSCPConfigFromMap(t *testing.T) {
	cfg := SCPConfigFromMap(map[string]interface{}{
		"default_user": "root",
		"scp_path":     "scp.exe",
		"allowed_local_path_prefixes": []any{"D:/deploy"},
		"allowed_remote_path_prefixes": []any{"/data/tmp"},
		"max_file_bytes": float64(1024),
	})
	if cfg.DefaultUser != "root" || cfg.SCPPath != "scp.exe" {
		t.Fatalf("unexpected base config: %#v", cfg)
	}
	if len(cfg.AllowedLocalPathPrefixes) != 1 || cfg.AllowedRemotePathPrefixes[0] != "/data/tmp" {
		t.Fatalf("unexpected prefixes: %#v", cfg)
	}
	if cfg.MaxFileBytes != 1024 {
		t.Fatalf("max file bytes = %d", cfg.MaxFileBytes)
	}
}

func TestRegisterSCPPasswordOnly(t *testing.T) {
	err := RegisterSCPTool(NewRegistry(), &SCPConfig{
		SSHExecConfig: SSHExecConfig{
			DefaultPassword:       "secret",
			StrictHostKeyChecking: "no",
		},
	})
	if err != nil {
		t.Fatalf("RegisterSCPTool: %v", err)
	}
}
