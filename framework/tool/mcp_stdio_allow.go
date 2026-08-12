package tool

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	for _, suf := range []string{".cmd", ".exe", ".bat"} {
		if strings.HasSuffix(lower, suf) {
			lower = strings.TrimSuffix(lower, suf)
			break
		}
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

// ResolveStdioMcpCommand returns a path that os/exec can start on the current OS.
// Order: explicit absolute/relative LookPath → Windows shim extensions → well-known install dirs.
// Override with SATH_MCP_STDIO_NPX / SATH_MCP_STDIO_NODE / SATH_MCP_STDIO_NPM for bare names.
func ResolveStdioMcpCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("stdio mcp: command is required")
	}

	base := normalizeStdioMcpCommand(command)
	if override := stdioMcpCommandOverride(base); override != "" {
		if path, err := exec.LookPath(override); err == nil {
			return path, nil
		}
		if fileExists(override) {
			return override, nil
		}
	}

	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}

	if runtime.GOOS == "windows" {
		lower := strings.ToLower(filepath.Base(command))
		if !strings.Contains(lower, ".") {
			for _, ext := range []string{".cmd", ".exe", ".bat"} {
				candidate := command + ext
				if path, err := exec.LookPath(candidate); err == nil {
					return path, nil
				}
			}
		}
	}

	// Bare allowlisted tools: probe well-known install locations (IDE-launched
	// processes often miss a complete PATH on Windows/macOS).
	if command == base || (runtime.GOOS == "windows" && command == base+".cmd") {
		for _, candidate := range stdioMcpWellKnownPaths(base) {
			if path, err := exec.LookPath(candidate); err == nil {
				return path, nil
			}
			if fileExists(candidate) {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf(
		"stdio mcp: command %q not found on %s (install Node.js, fix PATH, or set SATH_MCP_STDIO_%s)",
		command, runtime.GOOS, strings.ToUpper(base),
	)
}

func stdioMcpCommandOverride(base string) string {
	switch base {
	case "npx":
		return strings.TrimSpace(os.Getenv("SATH_MCP_STDIO_NPX"))
	case "node":
		return strings.TrimSpace(os.Getenv("SATH_MCP_STDIO_NODE"))
	case "npm":
		return strings.TrimSpace(os.Getenv("SATH_MCP_STDIO_NPM"))
	default:
		return ""
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func stdioMcpWellKnownPaths(base string) []string {
	switch runtime.GOOS {
	case "windows":
		return stdioMcpWellKnownWindows(base)
	case "darwin":
		return stdioMcpWellKnownUnix(base, []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			"/usr/bin",
		})
	default: // linux and others (incl. Docker)
		return stdioMcpWellKnownUnix(base, []string{
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
		})
	}
}

func stdioMcpWellKnownUnix(base string, dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, filepath.Join(dir, base))
	}
	return out
}

func stdioMcpWellKnownWindows(base string) []string {
	var roots []string
	for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			roots = append(roots, filepath.Join(v, "nodejs"))
			if key == "LOCALAPPDATA" {
				roots = append(roots, filepath.Join(v, "Programs", "nodejs"))
			}
		}
	}
	// Common defaults when env is stripped (service / IDE).
	roots = append(roots,
		`C:\Program Files\nodejs`,
		`C:\Program Files (x86)\nodejs`,
		`D:\Program Files\nodejs`,
	)

	exts := []string{".cmd", ".exe"}
	out := make([]string, 0, len(roots)*len(exts))
	seen := map[string]struct{}{}
	for _, root := range roots {
		for _, ext := range exts {
			p := filepath.Join(root, base+ext)
			low := strings.ToLower(p)
			if _, ok := seen[low]; ok {
				continue
			}
			seen[low] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}
