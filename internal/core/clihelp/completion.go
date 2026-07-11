package clihelp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/config"
)

// TargetNames returns the configured target names of <tool>.yaml. Any error
// (missing/!parse) yields nil — completion must never disrupt a tab press.
func TargetNames(tool string) []string {
	infos, err := config.ListTargets(tool)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(infos))
	for _, e := range infos {
		out = append(out, e.Name)
	}
	return out
}

// ArgvCompleter completes a manual-argv CLI (dbcli, logcli) that uses
// DisableFlagParsing, so cobra can't complete flags/positionals on its own.
// It reads ONLY local config (the target list) — never the network.
type ArgvCompleter struct {
	Tool       string                        // config tool name, for the target list
	Known      []string                      // every first-token the binary accepts (driver/adapter names)
	Canonical  func(string) string           // first-token → canonical adapter/driver; nil = identity
	VerbsFor   func(adapter string) []string // verbs offered after a chosen (canonical) adapter; nil = none
	ValueFlags []string                      // flags that consume a following value
	Flags      []string                      // all flag names, for `--<TAB>` completion (fallback when FlagsFor is nil)
	// FlagsFor, when set, supplies the `--<TAB>` flag names for the current
	// context (canonical first token + verb, "" when absent) — e.g. to offer
	// verb-specific flags. Takes precedence over Flags.
	FlagsFor func(first, verb string) []string
}

// Complete is the cobra ValidArgsFunction (the I/O shell: reads the target list,
// then delegates to the pure complete()).
func (c ArgvCompleter) Complete(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	infos, _ := config.ListTargets(c.Tool) // err → nil infos → only "targets"/flags offered
	return c.complete(infos, args, toComplete)
}

// complete is the pure completion logic (target data injected, so it's testable
// without a config file on disk).
func (c ArgvCompleter) complete(infos []config.TargetInfo, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	const dir = cobra.ShellCompDirectiveNoFileComp

	// 1. Completing the value of a value-flag typed with a space (`--target <TAB>`).
	if n := len(args); n > 0 {
		last := args[n-1]
		switch last {
		case "--target":
			return c.targetsFor(infos, c.scanFirst(args)), dir // narrowed to the chosen driver's adapter
		case "--output":
			return []string{"json", "raw"}, dir
		}
		if c.isValueFlag(last) {
			return nil, dir // free-form / no useful set (--database, --table, --exclude, …)
		}
	}

	// 2. Flag-name completion (`--<TAB>`). FlagsFor (context-aware) wins over the
	// static Flags list when set.
	if strings.HasPrefix(toComplete, "-") {
		if c.FlagsFor != nil {
			pos := c.scanPositionals(args)
			first, verb := "", ""
			if len(pos) > 0 {
				first = c.canon(pos[0])
			}
			if len(pos) > 1 {
				verb = pos[1]
			}
			return c.FlagsFor(first, verb), dir
		}
		return c.Flags, dir
	}

	// 3. Positionals.
	first, verbSeen := c.scan(args)
	switch {
	case first == "":
		return c.firstCandidates(infos), dir
	case !verbSeen && c.VerbsFor != nil:
		return c.VerbsFor(c.canon(first)), dir
	default:
		return nil, dir
	}
}

func (c ArgvCompleter) canon(s string) string {
	if c.Canonical == nil {
		return s
	}
	if cc := c.Canonical(s); cc != "" {
		return cc
	}
	return s
}

// firstCandidates offers only adapters that appear in the config (configured
// drivers), each annotated with the target(s) it has, plus the reserved `targets`.
func (c ArgvCompleter) firstCandidates(infos []config.TargetInfo) []string {
	byAdapter := targetsByAdapter(infos)
	out := make([]string, 0, len(c.Known)+1)
	for _, name := range c.Known {
		targets := byAdapter[c.canon(name)]
		if len(targets) == 0 {
			continue // not configured → hide (3a)
		}
		out = append(out, name+"\t"+targetDesc(targets)) // 3b: show the target(s) in the description
	}
	sort.Strings(out)
	out = append(out, "targets\tlist configured targets")
	return out
}

// targetsFor narrows --target candidates to the chosen driver's adapter; with no
// driver yet it offers all. A lone match means the shell auto-fills it.
func (c ArgvCompleter) targetsFor(infos []config.TargetInfo, first string) []string {
	if first == "" {
		out := make([]string, 0, len(infos))
		for _, e := range infos {
			out = append(out, e.Name)
		}
		return out
	}
	want := c.canon(first)
	var out []string
	for _, e := range infos {
		if e.Adapter == want {
			out = append(out, e.Name)
		}
	}
	return out
}

// scanPositionals returns the positional tokens (skipping flags and the values
// of value-flags), in order: [0] is the driver/adapter, [1] the verb, etc.
func (c ArgvCompleter) scanPositionals(args []string) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if eq := strings.IndexByte(a, '='); eq < 0 && c.isValueFlag(a) && i+1 < len(args) {
				i++ // skip the separate value token
			}
			continue
		}
		pos = append(pos, a)
	}
	return pos
}

// scan finds the first positional (driver/adapter) and whether a second
// positional (a verb) has been typed.
func (c ArgvCompleter) scan(args []string) (first string, verbSeen bool) {
	pos := c.scanPositionals(args)
	if len(pos) > 0 {
		first = pos[0]
	}
	return first, len(pos) > 1
}

func (c ArgvCompleter) scanFirst(args []string) string { first, _ := c.scan(args); return first }

func (c ArgvCompleter) isValueFlag(name string) bool {
	for _, f := range c.ValueFlags {
		if f == name {
			return true
		}
	}
	return false
}

func targetsByAdapter(infos []config.TargetInfo) map[string][]string {
	m := map[string][]string{}
	for _, e := range infos {
		m[e.Adapter] = append(m[e.Adapter], e.Name)
	}
	return m
}

func targetDesc(targets []string) string {
	if len(targets) == 1 {
		return "target: " + targets[0]
	}
	return fmt.Sprintf("%d targets", len(targets))
}
