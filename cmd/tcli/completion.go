package main

import "github.com/spf13/cobra"

// completeCaseFile completes the <case-or-dir> positional with .yaml/.yml files
// (and directories), so tab-completion surfaces case files.
func completeCaseFile(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
}

// wireCompletions attaches positional + flag value completion to the subcommands.
// Each verb takes a case file/dir; --config-dir takes a directory.
func wireCompletions(root *cobra.Command) {
	dirComp := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	for _, sub := range root.Commands() {
		sub.ValidArgsFunction = completeCaseFile
	}
	_ = root.RegisterFlagCompletionFunc("config-dir", dirComp)
}
