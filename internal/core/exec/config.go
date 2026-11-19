package exec

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// SSHFromConfig builds an SSH runner from the standard connection keys in an env
// block: host (required), user (required), port (optional), identity_file
// (optional path; a leading ~ is expanded). Adapter-agnostic — any SSH-based
// adapter reads the same keys.
func SSHFromConfig(raw map[string]any) (Runner, error) {
	host, _ := raw["host"].(string)
	user, _ := raw["user"].(string)
	if host == "" || user == "" {
		return nil, errs.Config("SSH_NO_HOST_USER", "ssh env block needs 'host' and 'user'")
	}
	id, _ := raw["identity_file"].(string)
	return SSH{
		Host:         host,
		User:         user,
		Port:         intFrom(raw["port"]),
		IdentityFile: expandTilde(id),
	}, nil
}

func intFrom(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
