package tcli

import (
	"context"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// ExplainCases parses and resolves each step's target -> app/actor/env, without
// executing.
func ExplainCases(ctx context.Context, r *Runner, path string, tags []string) (any, int, error) {
	paths, err := DiscoverCases(path)
	if err != nil {
		return nil, errs.ExitConfig, err
	}
	type stepMap struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		App    string `json:"app,omitempty"`
		Actor  string `json:"actor,omitempty"`
		Target string `json:"target,omitempty"`
	}
	type caseExpl struct {
		Path         string    `json:"path"`
		Name         string    `json:"name"`
		AllowDBWrite bool      `json:"allow_db_write"`
		Steps        []stepMap `json:"steps"`
		Diagnostics  []string  `json:"diagnostics,omitempty"`
	}
	var out []caseExpl
	for _, p := range paths {
		c, err := ParseCase(p)
		if err != nil {
			return nil, errs.ExitConfig, err
		}
		if !c.TagMatch(tags) {
			continue
		}
		ce := caseExpl{Path: p, Name: c.Name, AllowDBWrite: c.Safety.AllowDBWrite, Diagnostics: c.Validate()}
		add := func(acts []Action) {
			for _, a := range acts {
				sm := stepMap{Name: a.Name, Type: a.actionKind()}
				switch {
				case a.API != nil:
					app, actor, _, _ := resolveAPITarget(c, a.API)
					sm.App, sm.Actor = app, actorLabel(actor)
				case a.DB != nil:
					sm.Target, _ = resolveTarget(a.DB.Target, c.DBs, "db")
				case a.Log != nil:
					sm.Target, _ = resolveTarget(a.Log.Target, c.Logs, "log")
				}
				ce.Steps = append(ce.Steps, sm)
			}
		}
		add(c.Setup)
		add(c.Steps)
		add(c.Assertions)
		add(c.Cleanup)
		out = append(out, ce)
	}
	return map[string]any{"cases": out}, errs.ExitOK, nil
}

func actorLabel(a Actor) string {
	if a.Name != "" {
		return a.Name
	}
	if a.IsInline() {
		return "inline-" + actorHash(a.Vars)
	}
	return ""
}
