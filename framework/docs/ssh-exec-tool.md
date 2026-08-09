# ssh_exec built-in tool

`ssh_exec` runs a remote SSH command through the local OpenSSH client and returns a structured result for agents.

## Registration

Framework usage:

```go
_ = tool.RegisterSSHExecTool(reg, &tool.SSHExecConfig{
    DefaultUser: "vrviu",
    AllowedHosts: []string{"10.18.240.0/24"},
    AllowedUsers: []string{"vrviu"},
    AllowedCommandPrefixes: []string{
        "journalctl -u archive-manager",
        "grep ",
    },
    StrictHostKeyChecking: "yes",
    DefaultTimeoutSec: 30,
})
```

Portal built-in tool config:

```json
{
  "func_path": "ssh_exec",
  "parameters": {
    "default_user": "vrviu",
    "allowed_hosts": ["10.18.240.0/24"],
    "allowed_users": ["vrviu"],
    "allowed_command_prefixes": ["journalctl -u archive-manager"],
    "strict_host_key_checking": "yes",
    "timeout_sec": 30
  }
}
```

## Tool Input

- `host`: required SSH host or IP.
- `command`: required remote command.
- `user`: optional when `default_user` is configured.
- `timeout_sec`: optional command timeout override.
- `strict_host_key_checking`: optional `yes`, `accept-new`, or `no`.
- `working_dir`: optional remote directory. The tool runs `cd <working_dir> && <command>`.

## Tool Output

```json
{
  "ok": false,
  "host": "10.18.240.12",
  "user": "vrviu",
  "command": "journalctl -u archive-manager --since '2 hours ago'",
  "exit_code": 255,
  "stdout": "",
  "stderr": "Permission denied (publickey,password).",
  "error_category": "auth_failed",
  "duration_ms": 1234
}
```

`error_category` values:

- `host_key_failed`
- `auth_failed`
- `timeout`
- `network_failed`
- `command_failed`
- `blocked_by_policy`
- `internal_error`
