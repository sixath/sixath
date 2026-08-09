package tool

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
)

func runNativeSCP(ctx context.Context, req scpRequest, cfg *SCPConfig) scpRunResult {
	start := time.Now()
	var out scpRunResult

	client, err := dialSSHClient(ctx, req.host, req.user, req.timeoutSec, &cfg.SSHExecConfig)
	if err != nil {
		out.ExitCode = -1
		out.Stderr = err.Error()
		out.Err = err
		out.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		out.Duration = time.Since(start)
		return out
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		out.ExitCode = -1
		out.Stderr = err.Error()
		out.Err = err
		out.Duration = time.Since(start)
		return out
	}
	defer sftpClient.Close()

	remotePath := cleanRemotePath(req.remotePath)
	switch req.direction {
	case "upload":
		out.BytesTransferred, err = sftpUploadFile(sftpClient, req.localPath, remotePath)
	case "download":
		out.BytesTransferred, err = sftpDownloadFile(sftpClient, remotePath, req.localPath)
	default:
		err = errors.New("scp: invalid direction")
	}

	out.Duration = time.Since(start)
	out.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	if err != nil {
		out.Err = err
		out.ExitCode = -1
		if out.Stderr == "" {
			out.Stderr = err.Error()
		}
		return out
	}
	out.ExitCode = 0
	return out
}

func sftpUploadFile(client *sftp.Client, localPath, remotePath string) (int64, error) {
	src, err := os.Open(localPath)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, errors.New("local_path is a directory")
	}

	if err := client.MkdirAll(path.Dir(remotePath)); err != nil {
		return 0, err
	}
	dst, err := client.Create(remotePath)
	if err != nil {
		return 0, err
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		return n, err
	}
	return n, nil
}

func sftpDownloadFile(client *sftp.Client, remotePath, localPath string) (int64, error) {
	src, err := client.Open(remotePath)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, errors.New("remote_path is a directory")
	}

	dst, err := os.Create(localPath)
	if err != nil {
		return 0, err
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		return n, err
	}
	return n, nil
}
