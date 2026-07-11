package dataease

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// requireCode asserts err is an *errs.Error carrying the given code.
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", code)
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *errs.Error, got %T: %v", err, err)
	}
	if e.Code != code {
		t.Fatalf("expected code %s, got %s (%v)", code, e.Code, err)
	}
}

// capOut captures a single Batch payload from a driver Run.
type capOut struct {
	payload  any
	warnings []string
	wrote    bool
}

func (o *capOut) Batch(payload any, warnings ...string) error {
	o.payload = payload
	o.warnings = warnings
	o.wrote = true
	return nil
}

// rawCapOut captures both Batch and Raw output (the insert verb uses Raw).
type rawCapOut struct {
	payload any
	text    string
	wrote   bool
}

func (o *rawCapOut) Batch(payload any, _ ...string) error {
	o.payload = payload
	o.wrote = true
	return nil
}

func (o *rawCapOut) Raw(s string) error {
	o.text += s
	o.wrote = true
	return nil
}

// dbcliInput builds a dbcli.Input for a dataease env at baseURL with the given args.
func dbcliInput(baseURL string, args ...string) dbcli.Input {
	return dbcli.Input{
		Target: config.Target{Name: "local", Adapter: "dataease", Raw: map[string]any{
			"base_url":       baseURL,
			"data_source_id": "ds-1",
			"session":        "dataease.local.session",
		}},
		Args: args,
	}
}

// writeLoginCredential writes a 0600 dataease login credential under
// $AIDEV_CLIS_HOME/credentials/<name>.
func writeLoginCredential(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"username":"encrypted-user","password":"encrypted-pass","loginType":0}`)
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
}
