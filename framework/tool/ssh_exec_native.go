package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHExecNativeConfig 配置纯 Go SSH 客户端（golang.org/x/crypto/ssh），无需系统 ssh 二进制。
// strict_host_key_checking 为 "no" 时使用 InsecureIgnoreHostKey（仅建议隔离环境）；
// 为 "yes" / "accept-new" 时必须提供 KnownHostsPath（accept-new 不会自动写文件，需事先维护 known_hosts）。
type SSHExecNativeConfig struct {
	PrivateKeyPaths []string
	KnownHostsPath  string
	Port            int
}

func validateSSHExecCryptoConfig(cfg *SSHExecConfig) error {
	if cfg == nil || !usesCryptoSSH(cfg) {
		return nil
	}
	if cfg.Native != nil && len(cfg.Native.PrivateKeyPaths) == 0 && strings.TrimSpace(cfg.DefaultPassword) == "" {
		return errors.New("ssh_exec: native needs private_key_paths when not using password")
	}
	kh := effectiveKnownHostsPath(cfg)
	if cfg.StrictHostKeyChecking != "no" && strings.TrimSpace(kh) == "" {
		return errors.New("ssh_exec: known_hosts_path required when strict_host_key_checking is yes or accept-new")
	}
	return nil
}

func effectiveKnownHostsPath(cfg *SSHExecConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.Native != nil && strings.TrimSpace(cfg.Native.KnownHostsPath) != "" {
		return strings.TrimSpace(cfg.Native.KnownHostsPath)
	}
	return strings.TrimSpace(cfg.KnownHostsPath)
}

func effectiveSSHPort(cfg *SSHExecConfig) int {
	if cfg == nil {
		return 22
	}
	if cfg.Native != nil && cfg.Native.Port > 0 {
		return cfg.Native.Port
	}
	if cfg.Port > 0 {
		return cfg.Port
	}
	return 22
}

func loadSSHSigners(paths []string) ([]ssh.Signer, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var signers []ssh.Signer
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read private key %q: %w", p, err)
		}
		s, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("parse private key %q: %w", p, err)
		}
		signers = append(signers, s)
	}
	if len(signers) == 0 {
		return nil, errors.New("no loadable private keys")
	}
	return signers, nil
}

func dialSSHClient(ctx context.Context, host, user string, timeoutSec int, cfg *SSHExecConfig) (*ssh.Client, error) {
	if cfg == nil {
		return nil, errors.New("ssh: config is nil")
	}
	var signers []ssh.Signer
	if cfg.Native != nil && len(cfg.Native.PrivateKeyPaths) > 0 {
		var err error
		signers, err = loadSSHSigners(cfg.Native.PrivateKeyPaths)
		if err != nil {
			return nil, err
		}
	}

	var auth []ssh.AuthMethod
	if len(signers) > 0 {
		auth = append(auth, ssh.PublicKeys(signers...))
	}
	if p := strings.TrimSpace(cfg.DefaultPassword); p != "" {
		auth = append(auth, ssh.Password(p))
	}
	if len(auth) == 0 {
		return nil, errors.New("ssh: no authentication (configure password or native private_key_paths)")
	}

	var hostKeyCallback ssh.HostKeyCallback
	switch cfg.StrictHostKeyChecking {
	case "no":
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	default:
		khPath := effectiveKnownHostsPath(cfg)
		hcb, err := knownhosts.New(khPath)
		if err != nil {
			return nil, fmt.Errorf("known_hosts: %w", err)
		}
		hostKeyCallback = hcb
	}

	port := effectiveSSHPort(cfg)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	clientCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(timeoutSec) * time.Second,
	}

	d := net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	netConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, host, clientCfg)
	if err != nil {
		netConn.Close()
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func runNativeSSH(ctx context.Context, req sshExecRequest, cfg *SSHExecConfig) SSHExecRunResult {
	start := time.Now()
	var out SSHExecRunResult

	client, err := dialSSHClient(ctx, req.host, req.user, req.timeoutSec, cfg)
	if err != nil {
		out.ExitCode = -1
		out.Stderr = err.Error()
		out.Err = err
		out.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		out.Duration = time.Since(start)
		return out
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		out.ExitCode = -1
		out.Stderr = err.Error()
		out.Err = err
		out.Duration = time.Since(start)
		return out
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	remoteCmd := buildSSHExecRemoteCommand(req)
	err = session.Run(remoteCmd)

	out.Stdout = stdout.String()
	out.Stderr = stderr.String()
	out.Duration = time.Since(start)
	out.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	if out.Stderr == "" && err != nil {
		out.Stderr = err.Error()
	}

	if err != nil {
		out.Err = err
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			out.ExitCode = exitErr.ExitStatus()
		} else {
			out.ExitCode = -1
		}
		return out
	}
	out.ExitCode = 0
	return out
}
