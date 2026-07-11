package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestWriteCmdErr_RawIsPlainLine(t *testing.T) {
	var b bytes.Buffer
	writeCmdErr(&b, true, false, "NO_ADAPTER", "missing <adapter>")
	if b.String() != "ERROR NO_ADAPTER: missing <adapter>\n" {
		t.Fatalf("raw cmd err = %q", b.String())
	}
	b.Reset()
	writeCmdErr(&b, false, false, "NO_ADAPTER", "x")
	if !bytes.Contains(b.Bytes(), []byte(`"error"`)) {
		t.Fatalf("non-raw must be envelope: %q", b.String())
	}
}

func TestParseArgs_VerboseCount(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"none", []string{"kubectl", "logs"}, 0},
		{"single", []string{"kubectl", "-v", "logs"}, 1},
		{"double", []string{"kubectl", "-vv"}, 2},
		{"long", []string{"kubectl", "--verbose"}, 1},
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
	if got := parseArgs([]string{"local-file", "--output", "raw", "tail"}).output; got != "raw" {
		t.Fatalf("output=%q want raw", got)
	}
}

// Boolean/scalar flags given in --flag=value form must be parsed, not leaked
// into f.rest where they'd be forwarded to the adapter as a bogus token.
func TestParseArgs_EqFormNotLeaked(t *testing.T) {
	f := parseArgs([]string{"kubectl", "--pretty=true", "--verbose=2", "--output=json", "logs"})
	if !f.pretty {
		t.Errorf("--pretty=true not parsed")
	}
	if f.verbose != 2 {
		t.Errorf("--verbose=2 => verbose=%d want 2", f.verbose)
	}
	if f.output != "json" {
		t.Errorf("--output=json => output=%q want json", f.output)
	}
	if len(f.rest) != 1 || f.rest[0] != "logs" {
		t.Fatalf("rest=%v want [logs] (=-form flags leaked)", f.rest)
	}
	if f2 := parseArgs([]string{"kubectl", "--pretty=false", "logs"}); f2.pretty {
		t.Fatalf("--pretty=false not honored")
	}
}

// A value-flag must consume its following token even when that token begins
// with '-' (both space- and =-forms), so dash-prefixed values aren't rejected.
func TestParseArgs_DashValueConsumed(t *testing.T) {
	for _, args := range [][]string{
		{"kubectl", "--target", "-x", "logs"},
		{"kubectl", "--target=-x", "logs"},
	} {
		f := parseArgs(args)
		if f.target != "-x" {
			t.Errorf("args=%v => target=%q want -x", args, f.target)
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

func TestParseArgs_VerboseNotForwardedToAdapter(t *testing.T) {
	f := parseArgs([]string{"kubectl", "-v", "logs", "-l", "app=foo"})
	if f.adapter != "kubectl" {
		t.Fatalf("adapter=%q", f.adapter)
	}
	for _, r := range f.rest {
		if r == "-v" {
			t.Fatalf("-v leaked into adapter args: %v", f.rest)
		}
	}
}

func TestAdapterVerbs_SlsIsOnlySLSName(t *testing.T) {
	if got := adapterVerbs("sls"); len(got) == 0 {
		t.Fatal("sls should expose SLS verbs")
	}
	if got := adapterVerbs("aliyun-sls"); got != nil {
		t.Fatalf("old SLS adapter name should not be supported, got %v", got)
	}
}

func TestLogcliFlagsFor_SlsVerbFlags(t *testing.T) {
	got := logcliFlagsFor("sls", "search")
	for _, want := range []string{"--from", "--to", "--size", "--reverse"} {
		if !contains(got, want) {
			t.Fatalf("sls search flags missing %s: %v", want, got)
		}
	}
	if contains(logcliFlagsFor("sls", "trace"), "--reverse") {
		t.Fatalf("sls trace should not offer --reverse")
	}
	if !contains(logcliFlagsFor("sls", "tail"), "--interval") {
		t.Fatalf("sls tail should offer --interval")
	}
	if contains(logcliFlagsFor("kubectl", "logs"), "--from") {
		t.Fatalf("kubectl logs should not offer SLS flags")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// Legacy --env (renamed to --target) must be detected — not forwarded to the
// adapter, where it would be silently ignored and the query would run against
// default_target (the wrong-logstore trap).
func TestParseArgs_LegacyEnvDetected(t *testing.T) {
	f := parseArgs([]string{"sls", "--env", "qyry", "search", "x"})
	if !f.legacyEnv {
		t.Fatal("--env must set legacyEnv")
	}
	for _, a := range f.rest {
		if a == "--env" || a == "qyry" {
			t.Fatalf("--env and its value must not leak into adapter args: %v", f.rest)
		}
	}
	if f2 := parseArgs([]string{"sls", "--env=qyry", "search", "x"}); !f2.legacyEnv {
		t.Fatal("--env=value form must set legacyEnv")
	}
}

func TestParseArgs_FieldsFlag(t *testing.T) {
	f := parseArgs([]string{"sls", "--fields", "message,level", "search", "x"})
	if f.fields != "message,level" {
		t.Fatalf("fields = %q", f.fields)
	}
	for _, a := range f.rest {
		if a == "--fields" {
			t.Fatalf("--fields leaked to adapter args: %v", f.rest)
		}
	}
}
