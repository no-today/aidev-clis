package main

import (
	"strings"

	"github.com/spf13/cobra"
)

// configSubs are the verbs offered as the first token after `aidev config`.
// (restore also answers to import/use, but completion suggests the canonical name.)
var configSubs = []string{"backup", "restore"}

// configFlags returns the `--<TAB>` flag names for a chosen config verb.
// Keep in sync with runConfigBackup/runConfigRestore in internal/aidev/config.go.
func configFlags(sub string) []string {
	switch sub {
	case "backup":
		return []string{"--dest-dir", "--output", "--pretty"}
	case "restore":
		return []string{"--backup-dir", "--no-backup", "--output", "--pretty"}
	default:
		return nil
	}
}

// configValueFlags are the config flags that consume a following value token
// (so the positional scan skips that value).
var configValueFlags = map[string]bool{
	"--dest-dir": true, "--backup-dir": true, "--output": true,
}

// completeConfig is the cobra ValidArgsFunction for `aidev config`. The config
// command uses DisableFlagParsing (to hand raw args to internal/aidev.RunConfig
// unchanged), so cobra can't complete flags/positionals on its own — this fills
// the gap. args are the tokens after `config`.
func completeConfig(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeConfigArgs(args, toComplete)
}

// completeConfigArgs is the pure completion logic (no cobra.Command), so it is
// testable directly.
func completeConfigArgs(args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	const noFile = cobra.ShellCompDirectiveNoFileComp

	// 1. Value of a value-flag typed with a space (e.g. `--output <TAB>`).
	if n := len(args); n > 0 {
		switch args[n-1] {
		case "--output":
			return []string{"json", "raw"}, noFile
		case "--dest-dir", "--backup-dir":
			return nil, cobra.ShellCompDirectiveFilterDirs // a directory
		}
	}

	pos := scanConfigPositionals(args)
	sub := ""
	if len(pos) > 0 {
		sub = pos[0]
	}

	// 2. Flag-name completion (`--<TAB>`), scoped to the chosen verb.
	if strings.HasPrefix(toComplete, "-") {
		return configFlags(sub), noFile
	}

	// 3. Positionals.
	switch sub {
	case "":
		return configSubs, noFile // backup | restore
	case "restore":
		if len(pos) < 2 { // archive not yet given → complete file paths
			return nil, cobra.ShellCompDirectiveDefault
		}
		return nil, noFile
	default: // backup (or an unknown verb) → no further positionals
		return nil, noFile
	}
}

// scanConfigPositionals returns the positional tokens (skipping flags and the
// values of value-flags), in order: [0] is the verb, [1] the restore archive.
func scanConfigPositionals(args []string) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if eq := strings.IndexByte(a, '='); eq < 0 && configValueFlags[a] && i+1 < len(args) {
				i++ // skip the separate value token
			}
			continue
		}
		pos = append(pos, a)
	}
	return pos
}
