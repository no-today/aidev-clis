package dbcli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

type errDriver struct{ err error }

func (errDriver) Name() string                               { return "mysql" }
func (d errDriver) Run(context.Context, Input, Output) error { return d.err }

func doctorChecks(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("not a data envelope: %s", buf.String())
	}
	return env.Data
}

func TestDoctor_OK_NoSSH(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{okDriver{}}),
		RunArgs{Driver: "mysql", Target: "e", Args: []string{"doctor"}, Out: &buf})
	checks := doctorChecks(t, &buf)
	if code != 0 || len(checks) != 2 || checks[0]["name"] != "config" ||
		checks[1]["name"] != "connect" || checks[1]["ok"] != true {
		t.Fatalf("code=%d checks=%v", code, checks)
	}
}

func TestDoctor_SSHFail(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x', ssh: {host: h, user: u, identity_file: k}}\n")
	var buf bytes.Buffer
	d := errDriver{err: errs.Remote("SSH_TCP_DIAL", "dial bastion failed")}
	code := Run(context.Background(), NewRegistry([]Driver{d}),
		RunArgs{Driver: "mysql", Target: "e", Args: []string{"doctor"}, Out: &buf})
	checks := doctorChecks(t, &buf)
	if code != 5 || len(checks) != 2 || checks[1]["name"] != "ssh" || checks[1]["ok"] != false {
		t.Fatalf("code=%d checks=%v", code, checks)
	}
}

func TestDoctor_ConnectFail_WithSSH(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x', ssh: {host: h, user: u, identity_file: k}}\n")
	var buf bytes.Buffer
	d := errDriver{err: errs.Remote("DB_PING", "connection refused")}
	code := Run(context.Background(), NewRegistry([]Driver{d}),
		RunArgs{Driver: "mysql", Target: "e", Args: []string{"doctor"}, Out: &buf})
	checks := doctorChecks(t, &buf)
	// config ok → ssh ok (tunnel opened) → connect fail
	if code != 5 || len(checks) != 3 || checks[1]["name"] != "ssh" || checks[1]["ok"] != true ||
		checks[2]["name"] != "connect" || checks[2]["ok"] != false {
		t.Fatalf("code=%d checks=%v", code, checks)
	}
}
