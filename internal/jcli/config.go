// Package jcli is the jcli core: the Jenkins REST client, env config, and the
// service→job resolution. Single backend (Jenkins) — no adapter registry.
package jcli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Config is the typed view of a jcli env block (the Jenkins connection + groups).
type Config struct {
	BaseURL            string
	Credential         string
	InsecureSkipVerify bool
	CACert             string
	Groups             map[string]*Group // stack name → group (required; at least one)
}

// Group is one stack's job routing + post-build flow.
type Group struct {
	JobTemplate  string            // catch-all, "${service}" substituted
	JobTemplates map[string]string // service-prefix → template (longest wins)
	JobOverrides map[string]string // exact service → job name
	Deploy       *DeployConfig
	Vars         map[string]string
}

// DeployConfig is the parsed `deploy:` block (all optional).
type DeployConfig struct {
	Params       map[string]string
	Extract      map[string]string
	Steps        [][]string
	StepBinaries []string
	StepEnv      map[string]string
}

// ParseConfig converts an env's Raw block into a Config. Routing + deploy live in
// groups; a `groups:` block with at least one group is required.
func ParseConfig(raw map[string]any) (*Config, error) {
	c := &Config{Groups: map[string]*Group{}}
	c.BaseURL, _ = raw["base_url"].(string)
	if c.BaseURL == "" {
		return nil, errs.Config("CONFIG_INVALID", "jcli env missing 'base_url'")
	}
	c.Credential, _ = raw["credential"].(string)
	if c.Credential == "" {
		return nil, errs.Config("CONFIG_INVALID", "jcli env missing 'credential'")
	}
	c.InsecureSkipVerify, _ = raw["insecure_skip_verify"].(bool)
	c.CACert, _ = raw["ca_cert"].(string)
	g, ok := raw["groups"].(map[string]any)
	if !ok || len(g) == 0 {
		return nil, errs.Config("CONFIG_INVALID", "jcli env missing 'groups' (define at least one stack group)")
	}
	for name, gv := range g {
		gm, _ := gv.(map[string]any)
		c.Groups[name] = parseGroup(gm)
	}
	return c, nil
}

// parseGroup reads the routing + deploy + vars keys from a group block.
func parseGroup(m map[string]any) *Group {
	g := &Group{
		JobTemplates: strMap(m["job_templates"]),
		JobOverrides: strMap(m["job_overrides"]),
		Vars:         tildeMap(strMap(m["vars"])),
	}
	g.JobTemplate, _ = m["job_template"].(string)
	if d, ok := m["deploy"].(map[string]any); ok {
		g.Deploy = &DeployConfig{
			Params:       strMap(d["params"]),
			Extract:      strMap(d["extract"]),
			Steps:        parseSteps(d["steps"]),
			StepBinaries: strList(d["step_binaries"]),
			StepEnv:      strMap(d["step_env"]),
		}
	}
	return g
}

// Group returns the named group, or the sole group when name is empty. With
// multiple groups and no name it errors (Phase 2 adds auto-resolution).
func (c *Config) Group(name string) (*Group, error) {
	if name != "" {
		if g, ok := c.Groups[name]; ok {
			return g, nil
		}
		return nil, errs.Config("GROUP_UNKNOWN", "no group "+name+"; known: "+strings.Join(c.GroupNames(), ", "))
	}
	if len(c.Groups) == 1 {
		for _, g := range c.Groups {
			return g, nil
		}
	}
	return nil, errs.Config("GROUP_REQUIRED", "multiple groups; pass --group <name>: "+strings.Join(c.GroupNames(), ", "))
}

// AutoResolveGroup picks the group whose routing resolves <service> to a job that
// exists in the cache. Exactly one match → that group + job path; zero →
// SERVICE_UNKNOWN; more than one → GROUP_AMBIGUOUS; a nil cache → GROUP_REQUIRED.
func (c *Config) AutoResolveGroup(service string, cache *JobsCache) (*Group, string, error) {
	if cache == nil {
		return nil, "", errs.Config("GROUP_REQUIRED",
			"multiple groups; run `jobs --sync` to auto-resolve by service, or pass --group: "+strings.Join(c.GroupNames(), ", "))
	}
	var names []string
	var grp *Group
	var job string
	for name, g := range c.Groups {
		cand := g.ResolveJobName(service)
		if cache.HasPath(cand) {
			names = append(names, name)
			grp, job = g, cand
		}
	}
	sort.Strings(names)
	switch len(names) {
	case 1:
		return grp, job, nil
	case 0:
		return nil, "", errs.Config("SERVICE_UNKNOWN",
			"no group's job matches "+service+"; run `jobs --sync`, or pass --group/--job")
	default:
		return nil, "", errs.Config("GROUP_AMBIGUOUS",
			service+" resolves in multiple groups: "+strings.Join(names, ", ")+"; pass --group")
	}
}

// GroupForJob infers which group owns a raw job path (for `--job` without
// `--group`). A group claims the path if its routing would produce it for some
// service (an override value, or a template that reverses and round-trips through
// ResolveJobName). Exactly one claimant → that group; zero → JOB_NO_GROUP; many →
// GROUP_AMBIGUOUS. It never picks a wrong group — worst case it asks for --group.
func (c *Config) GroupForJob(jobPath string) (*Group, error) {
	var names []string
	var grp *Group
	for name, g := range c.Groups {
		if g.ClaimsJob(jobPath) {
			names = append(names, name)
			grp = g
		}
	}
	sort.Strings(names)
	switch len(names) {
	case 1:
		return grp, nil
	case 0:
		return nil, errs.Config("JOB_NO_GROUP", "job "+jobPath+" matches no group; pass --group")
	default:
		return nil, errs.Config("GROUP_AMBIGUOUS",
			"job "+jobPath+" matches multiple groups: "+strings.Join(names, ", ")+"; pass --group")
	}
}

// ClaimsJob reports whether this group's routing would produce jobPath for some
// service: an exact override value, or a template whose reversal round-trips
// through ResolveJobName (so override/longest-prefix precedence is respected).
func (g *Group) ClaimsJob(jobPath string) bool {
	for _, v := range g.JobOverrides {
		if v == jobPath {
			return true
		}
	}
	tmpls := make([]string, 0, len(g.JobTemplates)+1)
	if g.JobTemplate != "" {
		tmpls = append(tmpls, g.JobTemplate)
	}
	for _, t := range g.JobTemplates {
		tmpls = append(tmpls, t)
	}
	for _, t := range tmpls {
		if s, ok := reverseTemplate(t, jobPath); ok && g.ResolveJobName(s) == jobPath {
			return true
		}
	}
	return false
}

// reverseTemplate returns the service a "prefix${service}suffix" template needs to
// produce jobPath, and whether jobPath fits. A bare "${service}" (no prefix and no
// suffix) is too greedy to invert reliably and is treated as non-invertible, as is
// a template with zero or multiple "${service}" placeholders.
func reverseTemplate(tmpl, jobPath string) (string, bool) {
	const ph = "${service}"
	i := strings.Index(tmpl, ph)
	if i < 0 || strings.Contains(tmpl[i+len(ph):], ph) {
		return "", false // zero or multiple placeholders
	}
	prefix, suffix := tmpl[:i], tmpl[i+len(ph):]
	if prefix == "" && suffix == "" {
		return "", false // bare ${service}
	}
	if len(prefix)+len(suffix) >= len(jobPath) {
		return "", false
	}
	if !strings.HasPrefix(jobPath, prefix) || !strings.HasSuffix(jobPath, suffix) {
		return "", false
	}
	mid := jobPath[len(prefix) : len(jobPath)-len(suffix)]
	if mid == "" {
		return "", false
	}
	return mid, true
}

// GroupNames lists configured group names, sorted.
func (c *Config) GroupNames() []string {
	names := make([]string, 0, len(c.Groups))
	for n := range c.Groups {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolveJobName maps a service to a Jenkins job name. Precedence high→low:
// overrides (exact) → templates (longest prefix) → template (catch-all) → bare.
func (g *Group) ResolveJobName(service string) string {
	if name, ok := g.JobOverrides[service]; ok {
		return name
	}
	prefixes := make([]string, 0, len(g.JobTemplates))
	for p := range g.JobTemplates {
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	for _, p := range prefixes {
		if strings.HasPrefix(service, p) {
			return strings.ReplaceAll(g.JobTemplates[p], "${service}", service)
		}
	}
	if g.JobTemplate != "" {
		return strings.ReplaceAll(g.JobTemplate, "${service}", service)
	}
	return service
}

func strMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func strList(v any) []string {
	xs, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// parseSteps reads a list of argv lists; non-string scalars are coerced to string
// (so `[kubectl, scale, --replicas, 3]` keeps the 3).
func parseSteps(v any) [][]string {
	xs, ok := v.([]any)
	if !ok {
		return nil
	}
	var out [][]string
	for _, x := range xs {
		row, ok := x.([]any)
		if !ok {
			continue
		}
		argv := make([]string, 0, len(row))
		for _, tok := range row {
			if tok != nil {
				argv = append(argv, fmt.Sprint(tok))
			}
		}
		out = append(out, argv)
	}
	return out
}

// tildeMap expands a leading ~/ in each value to the user's home.
func tildeMap(m map[string]string) map[string]string {
	for k, v := range m {
		if strings.HasPrefix(v, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				m[k] = filepath.Join(home, v[2:])
			}
		}
	}
	return m
}
