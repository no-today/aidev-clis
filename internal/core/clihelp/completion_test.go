package clihelp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/config"
)

// a completer like dbcli's, with two configured adapters (mysql, postgres) and
// one that the binary knows but is NOT configured (redis → must be hidden).
func newCompleter() ArgvCompleter {
	return ArgvCompleter{
		Tool:       "dbcli",
		Known:      []string{"mysql", "postgres", "redis"},
		VerbsFor:   func(string) []string { return []string{"databases", "tables", "describe", "doctor", "insert"} },
		ValueFlags: []string{"--target", "--database", "--config-dir", "--output", "--timeout"},
		Flags:      []string{"--target", "--database", "--allow-write", "--pretty", "--timeout"},
	}
}

var fixtureTargets = []config.TargetInfo{
	{Name: "uat_mysql", Adapter: "mysql"},
	{Name: "prd_mysql", Adapter: "mysql"},
	{Name: "uat_pg", Adapter: "postgres"}, // postgres has exactly one target
}

func names(cands []string) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = strings.SplitN(c, "\t", 2)[0] // drop the description
	}
	return out
}

func TestComplete_FirstTokenOnlyConfiguredDrivers(t *testing.T) {
	got, dir := newCompleter().complete(fixtureTargets, nil, "")
	// redis is known but unconfigured → hidden; mysql/postgres + targets remain.
	if !reflect.DeepEqual(names(got), []string{"mysql", "postgres", "targets"}) {
		t.Fatalf("first token: got %v", names(got))
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive: got %v", dir)
	}
}

func TestComplete_FirstTokenDescriptionShowsSingleTarget(t *testing.T) {
	got, _ := newCompleter().complete(fixtureTargets, nil, "")
	var pgDesc, mysqlDesc string
	for _, c := range got {
		p := strings.SplitN(c, "\t", 2)
		switch p[0] {
		case "postgres":
			if len(p) > 1 {
				pgDesc = p[1]
			}
		case "mysql":
			if len(p) > 1 {
				mysqlDesc = p[1]
			}
		}
	}
	if pgDesc != "target: uat_pg" { // exactly one target → named
		t.Fatalf("postgres desc: got %q", pgDesc)
	}
	if mysqlDesc != "2 targets" { // multiple → count
		t.Fatalf("mysql desc: got %q", mysqlDesc)
	}
}

func TestComplete_TargetNarrowedToChosenDriverAdapter(t *testing.T) {
	// `dbcli mysql --target <TAB>` → only mysql targets (not the postgres one).
	got, _ := newCompleter().complete(fixtureTargets, []string{"mysql", "--target"}, "")
	if !reflect.DeepEqual(got, []string{"uat_mysql", "prd_mysql"}) {
		t.Fatalf("target narrowing: got %v", got)
	}
}

func TestComplete_TargetSingleMatchIsLoneCandidate(t *testing.T) {
	// postgres has one target → a single candidate, which the shell auto-fills.
	got, _ := newCompleter().complete(fixtureTargets, []string{"postgres", "--target"}, "")
	if !reflect.DeepEqual(got, []string{"uat_pg"}) {
		t.Fatalf("single-target auto: got %v", got)
	}
}

func TestComplete_TargetBeforeDriverOffersAll(t *testing.T) {
	got, _ := newCompleter().complete(fixtureTargets, []string{"--target"}, "")
	if !reflect.DeepEqual(got, []string{"uat_mysql", "prd_mysql", "uat_pg"}) {
		t.Fatalf("target before driver: got %v", got)
	}
}

func TestComplete_AfterDriverOffersVerbs(t *testing.T) {
	got, _ := newCompleter().complete(fixtureTargets, []string{"mysql"}, "")
	if !reflect.DeepEqual(got, []string{"databases", "tables", "describe", "doctor", "insert"}) {
		t.Fatalf("verbs: got %v", got)
	}
}

func TestComplete_AfterVerbOffersNothing(t *testing.T) {
	got, _ := newCompleter().complete(fixtureTargets, []string{"mysql", "describe"}, "")
	if got != nil {
		t.Fatalf("after verb: expected nil, got %v", got)
	}
}

func TestComplete_FlagNameCompletion(t *testing.T) {
	got, _ := newCompleter().complete(fixtureTargets, []string{"mysql"}, "--")
	if !reflect.DeepEqual(got, []string{"--target", "--database", "--allow-write", "--pretty", "--timeout"}) {
		t.Fatalf("flag names: got %v", got)
	}
}

func TestComplete_FlagsForContextAware(t *testing.T) {
	c := ArgvCompleter{
		Tool:       "dbcli",
		Known:      []string{"dataease", "mysql"},
		VerbsFor:   func(string) []string { return []string{"insert", "doctor"} },
		ValueFlags: []string{"--target", "--table", "--exclude"},
		FlagsFor: func(first, verb string) []string {
			f := []string{"--target"}
			if verb == "insert" {
				f = append(f, "--table")
				if first == "dataease" {
					f = append(f, "--exclude")
				}
			}
			return f
		},
	}
	infos := []config.TargetInfo{{Name: "de", Adapter: "dataease"}}

	// dataease insert → --table and --exclude offered.
	got, _ := c.complete(infos, []string{"dataease", "insert"}, "--")
	if !reflect.DeepEqual(got, []string{"--target", "--table", "--exclude"}) {
		t.Fatalf("dataease insert flags: got %v", got)
	}
	// mysql insert → --table but NOT --exclude.
	got, _ = c.complete(infos, []string{"mysql", "insert"}, "--")
	if !reflect.DeepEqual(got, []string{"--target", "--table"}) {
		t.Fatalf("mysql insert flags: got %v", got)
	}
	// dataease without a verb → no insert-only flags.
	got, _ = c.complete(infos, []string{"dataease"}, "--")
	if !reflect.DeepEqual(got, []string{"--target"}) {
		t.Fatalf("dataease (no verb) flags: got %v", got)
	}
	// value of --exclude → no candidates (not driver names).
	if got, _ := c.complete(infos, []string{"dataease", "insert", "--exclude"}, ""); got != nil {
		t.Fatalf("--exclude value: expected nil, got %v", got)
	}
}

func TestComplete_UsesSingleAdapterName(t *testing.T) {
	c := ArgvCompleter{
		Tool:       "logcli",
		Known:      []string{"sls", "kubectl"},
		VerbsFor:   func(a string) []string { return map[string][]string{"sls": {"search", "trace"}}[a] },
		ValueFlags: []string{"--target"},
	}
	infos := []config.TargetInfo{{Name: "prod", Adapter: "sls"}}
	// `logcli sls --target <TAB>` finds the SLS target directly.
	if got, _ := c.complete(infos, []string{"sls", "--target"}, ""); !reflect.DeepEqual(got, []string{"prod"}) {
		t.Fatalf("sls target: got %v", got)
	}
	// `logcli sls <TAB>` offers SLS verbs.
	if got, _ := c.complete(infos, []string{"sls"}, ""); !reflect.DeepEqual(got, []string{"search", "trace"}) {
		t.Fatalf("sls verbs: got %v", got)
	}
	// kubectl is unconfigured → hidden from the first token.
	if got, _ := c.complete(infos, nil, ""); !reflect.DeepEqual(names(got), []string{"sls", "targets"}) {
		t.Fatalf("first token (configured only): got %v", names(got))
	}
}

func TestComplete_OutputValues(t *testing.T) {
	got, _ := newCompleter().complete(fixtureTargets, []string{"mysql", "--output"}, "")
	want := []string{"json", "raw"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("--output completion = %v, want %v", names(got), want)
	}
}

func TestComplete_ContextFlagsAfterAdapterVerb(t *testing.T) {
	c := ArgvCompleter{
		Tool:       "logcli",
		Known:      []string{"sls"},
		VerbsFor:   func(a string) []string { return map[string][]string{"sls": {"search", "trace"}}[a] },
		ValueFlags: []string{"--target", "--from", "--to", "--size"},
		FlagsFor: func(first, verb string) []string {
			if first == "sls" && verb == "search" {
				return []string{"--target", "--from", "--to", "--size"}
			}
			return []string{"--target"}
		},
	}
	infos := []config.TargetInfo{{Name: "prod", Adapter: "sls"}}

	got, _ := c.complete(infos, []string{"sls", "search"}, "--")
	if !reflect.DeepEqual(got, []string{"--target", "--from", "--to", "--size"}) {
		t.Fatalf("sls search flags: got %v", got)
	}

	got, _ = c.complete(infos, []string{"sls", "--from", "1h", "search"}, "--")
	if !reflect.DeepEqual(got, []string{"--target", "--from", "--to", "--size"}) {
		t.Fatalf("sls leading range flags: got %v", got)
	}
}
