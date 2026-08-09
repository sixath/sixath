package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var stdioMcpEnvKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

var defaultStdioMcpAllowCmds = map[string]struct{}{
	"npx":  {},
	"node": {},
	"npm":  {},
}

// ValidateStdioMcp checks command basename against allowlist
// (npx|node|npm + SATH_MCP_STDIO_ALLOW_CMDS), args limits,
// dangerous flags (-e, --eval, --eval=), env key regex and denylist (PATH, LD_PRELOAD, DYLD_*).
func ValidateStdioMcp(command string, args []string, env map[string]string) error {
	base := normalizeStdioMcpCommand(command)
	if base == "" {
		return errors.New("stdio mcp: command is required")
	}
	if !isStdioMcpCommandAllowed(base) {
		return fmt.Errorf("stdio mcp: command %q not allowed", command)
	}
	if len(args) > 32 {
		return errors.New("stdio mcp: too many args")
	}
	for _, a := range args {
		if len(a) > 512 {
			return errors.New("stdio mcp: arg too long")
		}
		if isForbiddenStdioMcpArg(a) {
			return fmt.Errorf("stdio mcp: forbidden flag %q", a)
		}
	}
	for k := range env {
		if !stdioMcpEnvKeyRe.MatchString(k) {
			return fmt.Errorf("stdio mcp: invalid env key %q", k)
		}
		upper := strings.ToUpper(k)
		if upper == "PATH" || upper == "LD_PRELOAD" || strings.HasPrefix(upper, "DYLD_") {
			return fmt.Errorf("stdio mcp: forbidden env key %q", k)
		}
	}
	return nil
}

func isForbiddenStdioMcpArg(a string) bool {
	if a == "-e" || a == "--eval" {
		return true
	}
	return strings.HasPrefix(a, "--eval=")
}

func normalizeStdioMcpCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	base := filepath.Base(command)
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".cmd") {
		lower = strings.TrimSuffix(lower, ".cmd")
	}
	if strings.HasSuffix(lower, ".exe") {
		lower = strings.TrimSuffix(lower, ".exe")
	}
	return lower
}

func isStdioMcpCommandAllowed(base string) bool {
	if _, ok := defaultStdioMcpAllowCmds[base]; ok {
		return true
	}
	extra := os.Getenv("SATH_MCP_STDIO_ALLOW_CMDS")
	if extra == "" {
		return false
	}
	for _, part := range strings.Split(extra, ",") {
		normalized := normalizeStdioMcpCommand(part)
		if normalized == "" {
			continue
		}
		if normalized == base {
			return true
		}
	}
	return false
}
