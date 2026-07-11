package dataease

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Session is the persisted result of a DataEase login. It is bound to base_url
// so a token from one instance is never replayed against another.
type Session struct {
	Token      string `json:"token"`
	CapturedAt string `json:"captured_at,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
}

// validateName rejects names that could escape the sessions/credentials dir.
// Same rule as internal/core/cred so session and credential names behave alike.
func validateName(name string) error {
	if name == "" || name == "." || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return errs.Config("CRED_BAD_NAME", fmt.Sprintf("invalid name %q", name))
	}
	return nil
}

// SaveSession writes the session atomically (temp file + rename) at mode 0600,
// creating dir 0700, so a concurrent LoadSession never sees a half-written file.
func SaveSession(sess *Session, sessionKey, dir string) (string, error) {
	if err := validateName(sessionKey); err != nil {
		return "", err
	}
	if sess == nil {
		return "", errs.Auth("DATAEASE_SESSION_EMPTY", "dataease session is nil")
	}
	if sess.Token == "" {
		return "", errs.Auth("DATAEASE_SESSION_NO_TOKEN", "dataease session has no token")
	}
	if sess.CapturedAt == "" {
		sess.CapturedAt = time.Now().Format(time.RFC3339)
	}
	sess.SessionKey = sessionKey
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", errs.General("MKDIR_FAILED", err.Error())
	}
	path := filepath.Join(dir, sessionKey)
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return "", errs.General("SESSION_MARSHAL_FAILED", err.Error())
	}
	tmp, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return "", errs.General("WRITE_FAILED", err.Error())
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed away
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", errs.General("WRITE_FAILED", err.Error())
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", errs.General("WRITE_FAILED", err.Error())
	}
	if err := tmp.Close(); err != nil {
		return "", errs.General("WRITE_FAILED", err.Error())
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", errs.General("WRITE_FAILED", err.Error())
	}
	return path, nil
}

// LoadSession reads and validates the session: it must exist, carry a token, and
// (if recorded) match base_url and session_key. A missing file maps to
// DATAEASE_SESSION_MISSING so the caller can auto-login when a credential is set.
func LoadSession(sessionKey, baseURL, dir string) (*Session, error) {
	if err := validateName(sessionKey); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, sessionKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errs.Auth("DATAEASE_SESSION_MISSING", fmt.Sprintf("dataease session %q not found", sessionKey))
		}
		return nil, errs.Auth("DATAEASE_SESSION_READ_FAILED", err.Error())
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, errs.Auth("DATAEASE_SESSION_INVALID_JSON", fmt.Sprintf("dataease session file is not valid JSON: %v", err))
	}
	if sess.Token == "" {
		return nil, errs.Auth("DATAEASE_SESSION_NO_TOKEN", "dataease session has no token")
	}
	if sess.BaseURL != "" && baseURL != "" && sess.BaseURL != baseURL {
		return nil, errs.Auth("DATAEASE_SESSION_BASE_URL_MISMATCH",
			fmt.Sprintf("dataease session %q is for %s, not %s", sessionKey, sess.BaseURL, baseURL))
	}
	if sess.SessionKey != "" && sess.SessionKey != sessionKey {
		return nil, errs.Auth("DATAEASE_SESSION_KEY_MISMATCH",
			fmt.Sprintf("dataease session key metadata is %q, expected %q", sess.SessionKey, sessionKey))
	}
	return &sess, nil
}
