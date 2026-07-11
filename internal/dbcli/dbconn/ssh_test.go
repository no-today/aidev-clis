package dbconn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestLoadPrivateKey_NoPassphrase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "id")
	mustWriteKey(t, p, "")
	if _, err := LoadPrivateKey(p, ""); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPrivateKey_WithPassphrase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "id")
	mustWriteKey(t, p, "hunter2")
	if _, err := LoadPrivateKey(p, ""); err == nil {
		t.Fatal("encrypted key must require a passphrase")
	}
	if _, err := LoadPrivateKey(p, "hunter2"); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
}

func TestParseSSHConfig_XOR(t *testing.T) {
	if _, err := ParseSSHConfig(map[string]any{"host": "h", "user": "u"}); err == nil {
		t.Fatal("neither auth method must error")
	}
	if _, err := ParseSSHConfig(map[string]any{"host": "h", "user": "u", "identity_file": "k", "password_credential": "p"}); err == nil {
		t.Fatal("both auth methods must error")
	}
	c, err := ParseSSHConfig(map[string]any{"host": "h", "user": "u", "password_credential": "p"})
	if err != nil || c.PasswordCredential != "p" {
		t.Fatalf("password parse: %v %+v", err, c)
	}
}

func TestOpenTunnel_PasswordMissing(t *testing.T) {
	_, err := OpenTunnel(context.Background(), SSHConfig{Host: "127.0.0.1", User: "u", PasswordCredential: "x"}, "r", 1)
	if err == nil || !strings.Contains(err.Error(), "password_credential") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if ExpandHome("~/foo") != filepath.Join(home, "foo") || ExpandHome("/abs") != "/abs" {
		t.Fatal("ExpandHome")
	}
}

// mustWriteKey writes a fresh ed25519 OpenSSH key, optionally passphrase-encrypted.
func mustWriteKey(t *testing.T, path, passphrase string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "test")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
}
