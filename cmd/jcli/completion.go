package main

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/clihelp"
	"github.com/no-today/aidev-clis/internal/jcli"
)

// completeTarget suggests configured jcli target names for the --target flag.
func completeTarget(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return clihelp.TargetNames("jcli"), cobra.ShellCompDirectiveNoFileComp
}

// completeService suggests service (job leaf) names from the jobs cache. It
// scopes to --target when set, else unions every target's cache (deduped), and drops
// services already typed. Any error is swallowed — completion must never
// disrupt a tab press.
func completeService(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	typed := map[string]struct{}{}
	for _, a := range args {
		typed[a] = struct{}{}
	}

	targetFilter := ""
	if f := cmd.Flag("target"); f != nil {
		targetFilter = f.Value.String()
	}

	targets := clihelp.TargetNames("jcli")
	if targetFilter != "" {
		targets = []string{targetFilter}
	}

	seen := map[string]struct{}{}
	for _, t := range targets {
		cache, err := jcli.LoadJobsCache(t)
		if err != nil {
			continue // never synced for this target — skip, don't fail
		}
		for _, j := range cache.Jobs {
			seen[j.Name] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for s := range seen {
		if _, dup := typed[s]; dup {
			continue
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeJob suggests cached Jenkins job paths for --job. It mirrors
// completeService's target scoping, but uses Path because --job accepts the
// canonical Jenkins job path instead of a service leaf name.
func completeJob(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	targetFilter := ""
	if f := cmd.Flag("target"); f != nil {
		targetFilter = f.Value.String()
	}

	targets := clihelp.TargetNames("jcli")
	if targetFilter != "" {
		targets = []string{targetFilter}
	}

	seen := map[string]struct{}{}
	for _, t := range targets {
		cache, err := jcli.LoadJobsCache(t)
		if err != nil {
			continue
		}
		for _, j := range cache.Jobs {
			if j.Path == "" {
				continue
			}
			seen[j.Path] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeGroup suggests configured group (stack) names for --group, read from
// the target's jcli.yaml (local config only — no Jenkins call). Scoped to --target
// when set, else the default target. Errors are swallowed.
func completeGroup(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	targetFlag := ""
	if f := cmd.Flag("target"); f != nil {
		targetFlag = f.Value.String()
	}
	_, cfg, err := loadTarget(targetFlag)
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return cfg.GroupNames(), cobra.ShellCompDirectiveNoFileComp
}

// wireCompletions attaches target/service/group completion to the subcommands that
// take them. Service-taking verbs get the <service> positional completer; every
// verb with a --target / --group flag gets the matching value completion.
func wireCompletions(root *cobra.Command) {
	serviceVerb := map[string]bool{
		"build": true, "deploy": true, "status": true,
		"log": true, "cancel": true, "params": true,
	}
	dirComp := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	_ = root.RegisterFlagCompletionFunc("config-dir", dirComp)
	for _, sub := range root.Commands() {
		if sub.Flag("target") != nil {
			_ = sub.RegisterFlagCompletionFunc("target", completeTarget)
		}
		if sub.Flag("group") != nil {
			_ = sub.RegisterFlagCompletionFunc("group", completeGroup)
		}
		if sub.Flag("job") != nil {
			_ = sub.RegisterFlagCompletionFunc("job", completeJob)
		}
		if serviceVerb[sub.Name()] {
			sub.ValidArgsFunction = completeService
		}
	}
}
