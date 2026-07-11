package main

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestWriteCmdErr_RawIsPlainLine(t *testing.T) {
	var b bytes.Buffer
	writeCmdErr(&b, true, false, "NO_DRIVER", "missing <driver>")
	if b.String() != "ERROR NO_DRIVER: missing <driver>\n" {
		t.Fatalf("raw cmd err = %q", b.String())
	}
	b.Reset()
	writeCmdErr(&b, false, false, "NO_DRIVER", "x")
	if !bytes.Contains(b.Bytes(), []byte(`"error"`)) {
		t.Fatalf("non-raw must be envelope: %q", b.String())
	}
}

func TestDbcliVerbs(t *testing.T) {
	cases := map[string][]string{
		"dataease": {"doctor", "insert"},
		"mysql":    {"databases", "tables", "describe", "doctor", "insert"},
		"postgres": {"databases", "tables", "describe", "doctor", "insert"},
		"sqlite":   {"databases", "tables", "describe", "doctor", "insert"},
		"redis":    {"databases", "tables", "describe", "doctor"},
		"mongo":    {"databases", "tables", "describe", "doctor"},
	}
	for driver, want := range cases {
		if got := dbcliVerbs(driver); !reflect.DeepEqual(got, want) {
			t.Errorf("dbcliVerbs(%q) = %v, want %v", driver, got, want)
		}
	}
}

func TestDbcliFlags(t *testing.T) {
	base := []string{"--target", "--database", "--allow-write", "--pretty", "--verbose", "--timeout", "--config-dir", "--output"}
	cases := []struct {
		driver, verb string
		want         []string
	}{
		{"mysql", "", base},
		{"dataease", "doctor", base},
		{"mysql", "insert", append(append([]string{}, base...), "--table")},
		{"dataease", "insert", append(append([]string{}, base...), "--table", "--exclude")},
	}
	for _, c := range cases {
		if got := dbcliFlags(c.driver, c.verb); !reflect.DeepEqual(got, c.want) {
			t.Errorf("dbcliFlags(%q,%q) = %v, want %v", c.driver, c.verb, got, c.want)
		}
	}
}

func TestParseArgs_VerboseCount(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"none", []string{"mysql", "SELECT 1"}, 0},
		{"single short", []string{"mysql", "-v", "SELECT 1"}, 1},
		{"double short", []string{"mysql", "-vv"}, 2},
		{"repeated short", []string{"mysql", "-v", "-v"}, 2},
		{"long", []string{"mysql", "--verbose"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseArgs(c.args).verbose; got != c.want {
				t.Fatalf("verbose=%d want %d", got, c.want)
			}
		})
	}
}

func TestParseArgs_OutputRaw(t *testing.T) {
	if got := parseArgs([]string{"mysql", "--output", "raw", "SELECT 1"}).output; got != "raw" {
		t.Fatalf("output=%q want raw", got)
	}
}

// Boolean/scalar flags given in --flag=value form must be parsed, not leaked
// into f.rest where they'd be forwarded to the driver as a bogus SQL token.
func TestParseArgs_EqFormNotLeaked(t *testing.T) {
	f := parseArgs([]string{"mysql", "--pretty=true", "--verbose=2", "--allow-write=true", "--output=json", "SELECT 1"})
	if !f.pretty {
		t.Errorf("--pretty=true not parsed")
	}
	if f.verbose != 2 {
		t.Errorf("--verbose=2 => verbose=%d want 2", f.verbose)
	}
	if !f.allowWrite {
		t.Errorf("--allow-write=true not parsed")
	}
	if f.output != "json" {
		t.Errorf("--output=json => output=%q want json", f.output)
	}
	if len(f.rest) != 1 || f.rest[0] != "SELECT 1" {
		t.Fatalf("rest=%v want [SELECT 1] (=-form flags leaked)", f.rest)
	}
	if f2 := parseArgs([]string{"mysql", "--pretty=false", "--allow-write=false", "x"}); f2.pretty || f2.allowWrite {
		t.Fatalf("--pretty=false/--allow-write=false not honored: pretty=%v allowWrite=%v", f2.pretty, f2.allowWrite)
	}
}

// A value-flag must consume its following token even when that token begins
// with '-' (both space- and =-forms), so dash-prefixed values aren't rejected.
func TestParseArgs_DashValueConsumed(t *testing.T) {
	for _, args := range [][]string{
		{"mysql", "--database", "-x", "SELECT 1"},
		{"mysql", "--database=-x", "SELECT 1"},
	} {
		f := parseArgs(args)
		if f.database != "-x" {
			t.Errorf("args=%v => database=%q want -x", args, f.database)
		}
		if f.badFlag != "" {
			t.Errorf("args=%v => badFlag=%q want empty", args, f.badFlag)
		}
	}
}

// `targets -v` must carry diagnostics: emitTargets uses the Ctx writer.
func TestEmitTargets_CarriesDiagnostics(t *testing.T) {
	infos := []config.TargetInfo{{Name: "a"}, {Name: "b"}}

	var b bytes.Buffer
	ctx := diag.With(context.Background(), diag.New(1))
	if err := emitTargets(ctx, &b, infos, false, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Bytes(), []byte(`"diagnostics"`)) {
		t.Fatalf("targets -v output missing diagnostics: %s", b.String())
	}

	b.Reset()
	if err := emitTargets(context.Background(), &b, infos, false, false); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b.Bytes(), []byte(`"diagnostics"`)) {
		t.Fatalf("targets without -v must omit diagnostics: %s", b.String())
	}
}

func TestParseArgs_VerboseNotTreatedAsSQL(t *testing.T) {
	f := parseArgs([]string{"mysql", "-v", "SELECT 1"})
	if f.driver != "mysql" {
		t.Fatalf("driver=%q", f.driver)
	}
	for _, r := range f.rest {
		if r == "-v" {
			t.Fatalf("-v leaked into rest (would be run as SQL): %v", f.rest)
		}
	}
	if len(f.rest) != 1 || f.rest[0] != "SELECT 1" {
		t.Fatalf("rest=%v want [SELECT 1]", f.rest)
	}
}

func TestParseArgs_FileFlag(t *testing.T) {
	f := parseArgs([]string{"mysql", "--file", "q.sql"})
	if f.file != "q.sql" || len(f.rest) != 0 {
		t.Fatalf("file=%q rest=%v", f.file, f.rest)
	}
}

func TestReadSQLFile_PathAndStdin(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/q.sql"
	if err := os.WriteFile(p, []byte("select 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := readSQLFile(p)
	if err != nil || string(b) != "select 1" {
		t.Fatalf("readSQLFile: %q %v", b, err)
	}
	if _, err := readSQLFile(dir + "/missing.sql"); err == nil {
		t.Fatal("missing file must error")
	}
}
