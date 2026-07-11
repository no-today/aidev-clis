package dbconn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestResolve_Verbose_LogsSSHTunnel(t *testing.T) {
	dbAddr := startEchoTarget(t)
	dbHost, dbPort := splitHostPort(t, dbAddr)

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromSigner(priv)
	keyPath := writeSignerKey(t, priv)
	sshAddr := startTestSSHD(t, "", signer.PublicKey())
	sshHost, sshPort := splitHostPort(t, sshAddr)

	env := config.Target{Name: "e", Adapter: "mysql", Raw: map[string]any{
		"dsn": fmt.Sprintf("mysql://u@%s:%d/db", dbHost, dbPort),
		"ssh": map[string]any{
			"host": sshHost, "port": sshPort, "user": "u", "identity_file": keyPath,
		},
	}}

	d := diag.New(1)
	ctx := diag.With(context.Background(), d)
	u, cleanup, err := Resolve(ctx, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()

	lines := strings.Join(d.Lines(), "\n")
	if !strings.Contains(lines, "ssh tunnel") {
		t.Fatalf("ssh tunnel diagnostic must appear under -v: %q", lines)
	}
	// host must be rewritten to the local forward, not the real db host
	if u.Hostname() == dbHost && u.Port() == fmt.Sprint(dbPort) {
		t.Fatalf("dsn host should be rewritten to the local forward, got %s", u.Host)
	}
}

func TestResolve_NoSSH_OmitsTunnelLine(t *testing.T) {
	env := config.Target{Name: "e", Raw: map[string]any{"dsn": "mysql://u@h:3306/db"}}
	d := diag.New(2)
	ctx := diag.With(context.Background(), d)
	_, cleanup, err := Resolve(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if strings.Contains(strings.Join(d.Lines(), "\n"), "ssh tunnel") {
		t.Fatalf("no ssh block must not log a tunnel line: %v", d.Lines())
	}
}
