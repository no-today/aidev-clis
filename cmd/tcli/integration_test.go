package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/no-today/aidev-clis/internal/tcli"
)

// writeFake writes an executable fake CLI: routes by first arg, prints a canned JSON envelope.
func writeFake(t *testing.T, dir, name, script string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestIntegration_APIThenDB(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fakes")
	}
	dir := t.TempDir()
	// fake dbcli: targets returns mysql mapping; mysql query returns one row
	writeFake(t, dir, "dbcli", `case "$1" in
targets) echo '{"data":[{"name":"orders_uat","adapter":"mysql"}]}';;
*) echo '{"data":{"columns":["status"],"rows":[["confirmed"]]}}';;
esac`)
	// fake apicli: call returns 200 + body.id
	writeFake(t, dir, "apicli", `echo '{"data":{"status_code":200,"body":{"id":"ORD-1"}}}'`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// v2 schema: apps map, request: block, expect: expressions, capture: map
	caseYAML := `name: smoke
apps:
  orders: {}
dbs:
  main: orders_uat
steps:
  - name: create
    api:
      request: |
        POST /orders
      expect:
        - "status_code == 200"
      capture:
        oid: body.id
  - name: verify
    db:
      sql: "SELECT status FROM orders WHERE id='{{oid}}'"
      expect:
        - "count == 1"
        - "rows.0.0 == confirmed"
`
	cf := filepath.Join(dir, "smoke.yaml")
	if err := os.WriteFile(cf, []byte(caseYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	r := tcli.NewRunner("")
	payload, exit, err := tcli.RunCases(context.Background(), r, cf, nil)
	if err != nil {
		t.Fatal(err)
	}
	cr := payload.(tcli.CaseResult)
	if cr.Verdict != "PASS" || exit != 0 {
		t.Fatalf("verdict=%s exit=%d failure=%+v", cr.Verdict, exit, cr.Failure)
	}
}
