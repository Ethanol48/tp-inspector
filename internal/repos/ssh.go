package repos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ErrAuth means SSH refused the key: usually a passphrase-protected key that
// no agent holds, since tpi cannot type the passphrase (BatchMode).
var ErrAuth = errors.New("SSH key refused or locked")

// IsAuthError reports whether a git error is an SSH authentication failure.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuth) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "Permission denied (publickey") || strings.Contains(s, "Host key verification failed")
}

// CheckAccess tries to reach one remote without cloning it.
func CheckAccess(ctx context.Context, remote string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := git(ctx, os.TempDir(), "ls-remote", "-q", "--heads", remote)
	if IsAuthError(err) {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	return err
}

// Host returns the SSH host of a remote such as user@host:path or ssh://user@host/path.
func Host(remote string) string {
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		return u.Hostname()
	}
	h := remote
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	if i := strings.LastIndex(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	return h
}

// Identities returns the key files ssh would offer to host (from ~/.ssh/config
// and its defaults) that exist on disk.
func Identities(host string) []string {
	out, err := exec.Command("ssh", "-G", host).Output()
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	var keys []string
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 || f[0] != "identityfile" {
			continue
		}
		p := f[1]
		if strings.HasPrefix(p, "~/") {
			p = home + p[1:]
		}
		if _, err := os.Stat(p); err == nil {
			keys = append(keys, p)
		}
	}
	return keys
}

var agentVar = regexp.MustCompile(`(SSH_AUTH_SOCK|SSH_AGENT_PID)=([^;]+);`)

// EnsureAgent makes sure an ssh-agent is reachable through SSH_AUTH_SOCK,
// starting one for this process (and the git commands it runs) if needed.
// It returns the pid of the agent it started, or 0.
func EnsureAgent() (int, error) {
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if _, err := os.Stat(sock); err == nil {
			return 0, nil
		}
	}
	out, err := exec.Command("ssh-agent", "-s").Output()
	if err != nil {
		return 0, fmt.Errorf("ssh-agent: %v", err)
	}
	pid := 0
	for _, m := range agentVar.FindAllStringSubmatch(string(out), -1) {
		os.Setenv(m[1], m[2])
		if m[1] == "SSH_AGENT_PID" {
			fmt.Sscan(m[2], &pid)
		}
	}
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		return 0, fmt.Errorf("ssh-agent: unexpected output %q", out)
	}
	return pid, nil
}

// StopAgent stops an agent started by EnsureAgent.
func StopAgent(pid int) {
	if pid > 0 {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
}
