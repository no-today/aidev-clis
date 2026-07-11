package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/jcli"
)

// targetFlags are the three addressing flags shared by every service-scoped verb
// (build/deploy/cancel/status/log/params). bind wires them onto a command; the
// values are read back inside RunE (after cobra parses). --job always bypasses
// service resolution, which is why it feeds straight into resolveTarget.
type targetFlags struct {
	target, job, group string
}

func (f *targetFlags) bind(c *cobra.Command) {
	c.Flags().StringVar(&f.target, "target", "", "configured target")
	c.Flags().StringVar(&f.job, "job", "", "raw job name (bypass service resolution)")
	c.Flags().StringVar(&f.group, "group", "", "config group (stack) to resolve within")
}

// resolveTarget wraps resolveTargetInner to log the resolved target/job once, for
// every command that resolves a target (DRY across build/deploy/status/...).
func resolveTarget(flagTarget, service, jobFlag, groupFlag string) (*jcli.Client, string, string, *jcli.Group, error) {
	cl, job, targetName, grp, err := resolveTargetInner(flagTarget, service, jobFlag, groupFlag)
	if err == nil {
		dg.Logf(1, "resolved target=%s job=%s", targetName, job)
	}
	return cl, job, targetName, grp, err
}

func resolveTargetInner(flagTarget, service, jobFlag, groupFlag string) (*jcli.Client, string, string, *jcli.Group, error) {
	target, cfg, err := loadTarget(flagTarget)
	if err != nil {
		return nil, "", "", nil, err
	}
	cl, err := jcli.NewClient(cfg)
	if err != nil {
		return nil, "", "", nil, err
	}
	// --job fully bypasses routing. A group is attached when unambiguous: explicit
	// --group, a sole group, or — on a multi-group target — inferred from the job path
	// (the group whose routing would produce it). Commands that need a group
	// (deploy) validate it; build/status/log/cancel/params ignore it.
	if jobFlag != "" {
		grp, gerr := cfg.Group(groupFlag)
		if gerr != nil {
			if groupFlag != "" {
				return nil, "", "", nil, gerr // an explicit --group that doesn't exist
			}
			grp, _ = cfg.GroupForJob(jobFlag) // infer; nil if not inferrable (deploy validates)
		}
		return cl, jobFlag, target.Name, grp, nil
	}
	grp, gerr := cfg.Group(groupFlag)
	if gerr != nil {
		// Phase 2: multiple groups + no --group → auto-resolve the group from the
		// service name via the synced jobs cache.
		if groupFlag == "" && service != "" {
			cache, _ := jcli.LoadJobsCache(target.Name) // nil if never synced
			g, job, aerr := cfg.AutoResolveGroup(service, cache)
			if aerr != nil {
				return nil, "", "", nil, aerr
			}
			return cl, job, target.Name, g, nil
		}
		return nil, "", "", nil, gerr
	}
	if service == "" {
		return nil, "", "", nil, errs.Config("NO_TARGET", "give a <service> or --job <name>")
	}
	job := grp.ResolveJobName(service)
	if job == service { // no template matched → try the synced jobs cache
		if cache, err := jcli.LoadJobsCache(target.Name); err == nil {
			switch hits := cache.FindByName(service); len(hits) {
			case 1:
				job = hits[0].Path
			case 0:
				// keep the bare service name
			default:
				paths := make([]string, len(hits))
				for i, h := range hits {
					paths[i] = h.Path
				}
				return nil, "", "", nil, errs.Config("JOB_AMBIGUOUS",
					"service "+service+" matches multiple jobs: "+strings.Join(paths, ", ")+"; use --job <path>")
			}
		}
	}
	return cl, job, target.Name, grp, nil
}

func parseParams(kvs []string) (map[string]string, error) {
	m := map[string]string{}
	for _, kv := range kvs {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			return nil, errs.Config("BAD_PARAM", "param must be k=v: "+kv)
		}
		m[kv[:i]] = kv[i+1:]
	}
	return m, nil
}

func first(a []string) string {
	if len(a) > 0 {
		return a[0]
	}
	return ""
}
