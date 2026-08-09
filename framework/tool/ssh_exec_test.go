package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeSSHRunner struct {
	name       string
	args       []string
	workingDir string
	result     SSHExecRunResult
}

func (f *fakeSSHRunner) Run(ctx context.Context, name string, args []string, workingDir string) SSHExecRunResult {
	f.name = name
	f.args = append([]string{}, args...)
	f.workingDir = workingDir
	return f.result
}

func TestSSHExec_DefaultHostFromSingleAllowed(t *testing.T) {
	runner := &fakeSSHRunner{result: SSHExecRunResult{ExitCode: 0, Stdout: "ok\n"}}
	reg := NewRegistry()
	err := RegisterSSHExecTool(reg, &SSHExecConfig{
		Runner:                 runner,
		DefaultUser:            "u",
		AllowedHosts:           []string{"10.0.0.5"},
		AllowedUsers:           []string{"u"},
		AllowedCommandPrefixes: []string{"echo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ssh_exec")
	_, err = tl.Execute(context.Background(), map[string]any{
		"command": "echo hi",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	joined := strings.Join(runner.args, " ")
	if !strings.Contains(joined, "u@10.0.0.5") {
		t.Fatalf("expected implicit host in ssh target, args=%q", joined)
	}
}

func TestSSHExec_HostSynonymServer(t *testing.T) {
	runner := &fakeSSHRunner{result: SSHExecRunResult{ExitCode: 0, Stdout: "ok\n"}}
	reg := NewRegistry()
	_ = RegisterSSHExecTool(reg, &SSHExecConfig{
		Runner:                 runner,
		DefaultUser:            "u",
		AllowedHosts:           []string{"mybox"},
		AllowedUsers:           []string{"u"},
		AllowedCommandPrefixes: []string{"echo"},
	})
	tl, _ := reg.Get("ssh_exec")
	_, err := tl.Execute(context.Background(), map[string]any{
		"server":  "mybox",
		"command": "echo hi",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(strings.Join(runner.args, " "), "u@mybox") {
		t.Fatalf("expected server as host, args=%v", runner.args)
	}
}

func TestSSHExec_BasicSuccessUsesRunner(t *testing.T) {
	runner := &fakeSSHRunner{
		result: SSHExecRunResult{
			ExitCode: 0,
			Stdout:   "archive-manager ok\n",
			Duration: 25 * time.Millisecond,
		},
	}
	reg := NewRegistry()
	err := RegisterSSHExecTool(reg, &SSHExecConfig{
		Runner:                 runner,
		SSHPath:                "ssh-custom",
		DefaultUser:            "vrviu",
		DefaultTimeoutSec:      12,
		StrictHostKeyChecking:  "accept-new",
		AllowedHosts:           []string{"10.18.240.*"},
		AllowedUsers:           []string{"vrviu"},
		AllowedCommandPrefixes: []string{"journalctl -u archive-manager", "grep "},
	})
	if err != nil {
		t.Fatalf("RegisterSSHExecTool: %v", err)
	}
	tl, ok := reg.Get("ssh_exec")
	if !ok {
		t.Fatal("ssh_exec not found")
	}

	out, err := tl.Execute(context.Background(), map[string]any{
		"host":    "10.18.240.104",
		"command": "journalctl -u archive-manager --since '2 hours ago'",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res, ok := out.(SSHExecResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", out)
	}
	if !res.OK || res.ExitCode != 0 || res.Stdout != "archive-manager ok\n" || res.ErrorCategory != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if runner.name != "ssh-custom" {
		t.Fatalf("runner name = %q", runner.name)
	}
	joined := strings.Join(runner.args, " ")
	for _, want := range []string{
		"-o StrictHostKeyChecking=accept-new",
		"-o BatchMode=yes",
		"-o ConnectTimeout=12",
		"vrviu@10.18.240.104",
		"journalctl -u archive-manager",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ssh args %q missing %q", joined, want)
		}
	}
}

func TestSSHExec_BlockedByPolicy(t *testing.T) {
	reg := NewRegistry()
	_ = RegisterSSHExecTool(reg, &SSHExecConfig{
		Runner:                 &fakeSSHRunner{},
		AllowedHosts:           []string{"10.18.240.0/24"},
		AllowedUsers:           []string{"vrviu"},
		AllowedCommandPrefixes: []string{"journalctl -u archive-manager"},
	})
	tl, _ := reg.Get("ssh_exec")

	out, err := tl.Execute(context.Background(), map[string]any{
		"host":    "10.18.241.9",
		"user":    "root",
		"command": "rm -rf /",
	})
	if err != nil {
		t.Fatalf("Execute should return structured policy result: %v", err)
	}
	res := out.(SSHExecResult)
	if res.OK || res.ErrorCategory != SSHExecErrorBlockedByPolicy {
		t.Fatalf("unexpected policy result: %+v", res)
	}
	if !strings.Contains(res.Stderr, "host") {
		t.Fatalf("expected policy reason in stderr, got %q", res.Stderr)
	}
}

func TestSSHExec_ClassifiesFailures(t *testing.T) {
	cases := []struct {
		name    string
		run     SSHExecRunResult
		wantCat string
	}{
		{
			name: "host key",
			run: SSHExecRunResult{
				ExitCode: 255,
				Stderr:   "Host key verification failed.",
			},
			wantCat: SSHExecErrorHostKeyFailed,
		},
		{
			name: "auth",
			run: SSHExecRunResult{
				ExitCode: 255,
				Stderr:   "Permission denied (publickey,password).",
			},
			wantCat: SSHExecErrorAuthFailed,
		},
		{
			name: "timeout",
			run: SSHExecRunResult{
				ExitCode: -1,
				TimedOut: true,
				Stderr:   "context deadline exceeded",
			},
			wantCat: SSHExecErrorTimeout,
		},
		{
			name: "network",
			run: SSHExecRunResult{
				ExitCode: 255,
				Stderr:   "ssh: connect to host 10.18.240.12 port 22: Connection refused",
			},
			wantCat: SSHExecErrorNetworkFailed,
		},
		{
			name: "command",
			run: SSHExecRunResult{
				ExitCode: 1,
				Stderr:   "Unit archive-manager.service could not be found.",
			},
			wantCat: SSHExecErrorCommandFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySSHExecRun(tc.run)
			if got != tc.wantCat {
				t.Fatalf("classifySSHExecRun() = %q, want %q", got, tc.wantCat)
			}
		})
	}
}

func TestSSHExecConfigFromMap(t *testing.T) {
	cfg := SSHExecConfigFromMap(map[string]interface{}{
		"parameters": map[string]interface{}{
			"ssh_path":                 "ssh.exe",
			"default_user":             "vrviu",
			"timeout_sec":              9,
			"strict_host_key_checking": "yes",
			"allowed_hosts":            []interface{}{"10.18.240.*"},
			"allowed_users":            []interface{}{"vrviu"},
			"allowed_command_prefixes": []interface{}{"journalctl"},
			"denied_command_patterns":  []interface{}{"rm\\s+-rf"},
		},
	})
	if cfg.SSHPath != "ssh.exe" || cfg.DefaultUser != "vrviu" || cfg.DefaultTimeoutSec != 9 {
		t.Fatalf("unexpected scalar config: %+v", cfg)
	}
	if cfg.StrictHostKeyChecking != "yes" {
		t.Fatalf("strict host key config = %q", cfg.StrictHostKeyChecking)
	}
	if len(cfg.AllowedHosts) != 1 || cfg.AllowedHosts[0] != "10.18.240.*" {
		t.Fatalf("allowed hosts: %+v", cfg.AllowedHosts)
	}
	if len(cfg.AllowedCommandPrefixes) != 1 || cfg.AllowedCommandPrefixes[0] != "journalctl" {
		t.Fatalf("allowed prefixes: %+v", cfg.AllowedCommandPrefixes)
	}
}

func TestSSHExecConfigFromMapNative(t *testing.T) {
	cfg := SSHExecConfigFromMap(map[string]interface{}{
		"strict_host_key_checking": "yes",
		"native": map[string]interface{}{
			"private_key_path": "/tmp/id",
			"known_hosts_path": "/tmp/kh",
			"port":             2222,
		},
	})
	if cfg.Native == nil || len(cfg.Native.PrivateKeyPaths) != 1 || cfg.Native.PrivateKeyPaths[0] != "/tmp/id" {
		t.Fatalf("native paths: %+v", cfg.Native)
	}
	if cfg.Native.KnownHostsPath != "/tmp/kh" || cfg.Native.Port != 2222 {
		t.Fatalf("native: %+v", cfg.Native)
	}
}

func TestRegisterSSHExecNativeValidation(t *testing.T) {
	reg := NewRegistry()
	err := RegisterSSHExecTool(reg, &SSHExecConfig{
		Native:                &SSHExecNativeConfig{PrivateKeyPaths: []string{"/x"}},
		StrictHostKeyChecking: "yes",
	})
	if err == nil {
		t.Fatal("expected error for missing known_hosts with strict yes")
	}
	err = RegisterSSHExecTool(NewRegistry(), &SSHExecConfig{
		Native:                &SSHExecNativeConfig{PrivateKeyPaths: []string{"/x"}, KnownHostsPath: "/kh"},
		StrictHostKeyChecking: "yes",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestRegisterSSHExecPasswordOnly(t *testing.T) {
	err := RegisterSSHExecTool(NewRegistry(), &SSHExecConfig{
		DefaultUser:           "vrviu",
		DefaultPassword:       "secret",
		Port:                  2222,
		StrictHostKeyChecking: "no",
	})
	if err != nil {
		t.Fatalf("RegisterSSHExecTool: %v", err)
	}
}

func TestBuildSSHExecArgsPort(t *testing.T) {
	cfg := &SSHExecConfig{Port: 2222}
	args := buildSSHExecArgs(sshExecRequest{
		host:                  "h",
		user:                  "u",
		command:               "true",
		timeoutSec:            9,
		strictHostKeyChecking: "no",
	}, cfg)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-p 2222") {
		t.Fatalf("want -p 2222 in %v", args)
	}
}

func TestSSHExecEffectiveConfig_PasswordFromContext(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := WithSecretProvider(context.Background(), store)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess_1")
	if err := store.PutSecret(ctx, "sess_1", "ssh_password", "ctxpass", time.Minute); err != nil {
		t.Fatal(err)
	}

	cfg := &SSHExecConfig{
		DefaultUser:           "u",
		StrictHostKeyChecking: "no",
	}
	eff := sshExecEffectiveConfig(ctx, cfg)
	if eff.DefaultPassword != "ctxpass" {
		t.Fatalf("password = %q", eff.DefaultPassword)
	}
	if !usesCryptoSSH(eff) {
		t.Fatal("expected crypto ssh when password provided via context")
	}
}

func TestSSHExecEffectiveConfig_ConfigPasswordWinsOverContext(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := WithSecretProvider(context.Background(), store)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess_1")
	_ = store.PutSecret(ctx, "sess_1", "ssh_password", "ctxpass", time.Minute)

	cfg := &SSHExecConfig{
		DefaultUser:           "u",
		DefaultPassword:       "configpass",
		StrictHostKeyChecking: "no",
	}
	eff := sshExecEffectiveConfig(ctx, cfg)
	if eff.DefaultPassword != "configpass" {
		t.Fatalf("config password should win, got %q", eff.DefaultPassword)
	}
}

func TestSSHExecEffectiveConfig_PasswordAliasField(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := WithSecretProvider(context.Background(), store)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess_1")
	_ = store.PutSecret(ctx, "sess_1", "password", "aliaspass", time.Minute)

	eff := sshExecEffectiveConfig(ctx, &SSHExecConfig{
		DefaultUser:           "u",
		StrictHostKeyChecking: "no",
	})
	if eff.DefaultPassword != "aliaspass" {
		t.Fatalf("password = %q", eff.DefaultPassword)
	}
}
