package apicli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Session is the persisted result of a login: captured vars (token/cookie).
type Session struct {
	Vars   map[string]string `json:"vars"`
	Cookie string            `json:"cookie,omitempty"`
}

// sessionPath is ~/.aidev-clis/sessions/<app>/<actor>/<env>. Default env ("")
// is stored as "_default" so it never collides with a real env name.
func sessionPath(tg *Target) string {
	home, _ := config.Home()
	env := tg.Env
	if env == "" {
		env = "_default"
	}
	return filepath.Join(home, "sessions", tg.App, tg.Actor, env)
}

// LoadSession reads a stored session; ok=false if none.
func LoadSession(tg *Target) (Session, bool) {
	data, err := os.ReadFile(sessionPath(tg))
	if err != nil {
		return Session{}, false
	}
	var s Session
	if json.Unmarshal(data, &s) != nil {
		return Session{}, false
	}
	return s, true
}

// SaveSession writes the session atomically (temp file + rename) at mode 0600,
// creating parent dirs 0700. Atomic so a concurrent LoadSession never sees a
// half-written file.
func SaveSession(tg *Target, s Session) error {
	p := sessionPath(tg)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errs.General("SESSION_WRITE", err.Error())
	}
	data, err := json.Marshal(s)
	if err != nil {
		return errs.General("SESSION_WRITE", err.Error())
	}
	tmp, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return errs.General("SESSION_WRITE", err.Error())
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed away
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return errs.General("SESSION_WRITE", err.Error())
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errs.General("SESSION_WRITE", err.Error())
	}
	if err := tmp.Close(); err != nil {
		return errs.General("SESSION_WRITE", err.Error())
	}
	if err := os.Rename(tmpName, p); err != nil {
		return errs.General("SESSION_WRITE", err.Error())
	}
	return nil
}

// SessionAgeSeconds returns the age of the stored session in seconds, and false
// if there is no session file.
func SessionAgeSeconds(tg *Target) (int64, bool) {
	info, err := os.Stat(sessionPath(tg))
	if err != nil {
		return 0, false
	}
	return int64(time.Since(info.ModTime()).Seconds()), true
}

// DeleteSession removes the stored session (no error if already absent).
func DeleteSession(tg *Target) error {
	if err := os.Remove(sessionPath(tg)); err != nil && !os.IsNotExist(err) {
		return errs.General("SESSION_DELETE", err.Error())
	}
	return nil
}
