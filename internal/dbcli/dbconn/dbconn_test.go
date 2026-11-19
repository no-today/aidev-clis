package dbconn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
)

func writeCred(t *testing.T, name, body string) {
	t.Helper()
	home := os.Getenv("AIDEV_CLIS_HOME")
	dir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_InjectsPassword(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "db.pw", "s3cret")
	env := config.Target{Name: "x", Adapter: "mysql", Raw: map[string]any{
		"dsn":        "mysql://app_ro@10.0.0.1:3306/orders",
		"credential": "db.pw",
	}}
	u, cleanup, err := Resolve(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	pw, _ := u.User.Password()
	if u.User.Username() != "app_ro" || pw != "s3cret" {
		t.Fatalf("userinfo not injected: %s", u.Redacted())
	}
	if u.Host != "10.0.0.1:3306" {
		t.Fatalf("host: %s", u.Host)
	}
}

func TestResolve_MissingDSN(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	_, _, err := Resolve(context.Background(), config.Target{Raw: map[string]any{"credential": "x"}})
	if err == nil {
		t.Fatal("missing dsn must error")
	}
}

func TestResolve_SSHMalformedErrors(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "db.pw", "s3cret")
	// A malformed ssh block (missing 'host') must return SSH_HOST_MISSING.
	env := config.Target{Raw: map[string]any{
		"dsn":        "mysql://u@h:3306/d",
		"credential": "db.pw",
		"ssh":        map[string]any{"user": "d", "identity_file": "~/.ssh/id_rsa"},
	}}
	_, _, err := Resolve(context.Background(), env)
	if err == nil {
		t.Fatal("ssh block missing 'host' must error")
	}
	if !strings.Contains(err.Error(), "SSH_HOST_MISSING") {
		t.Fatalf("expected SSH_HOST_MISSING, got: %v", err)
	}
}

func TestTrimCred(t *testing.T) {
	cases := map[string]string{
		"pw\n":     "pw",
		"pw\r\n":   "pw",
		"pw":       "pw",
		"pw\n\n":   "pw",
		" pw ":     " pw ", // legit surrounding spaces preserved
		"a b\nc\n": "a b\nc",
	}
	for in, want := range cases {
		if got := trimCred([]byte(in)); got != want {
			t.Errorf("trimCred(%q)=%q want %q", in, got, want)
		}
	}
}
