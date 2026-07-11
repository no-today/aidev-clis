package tcli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// seedBuiltins seeds run_id/uuid/now into vars (so a one-shot gate can mint
// collision-free ids each run).
func seedBuiltins(vars map[string]string, runID string) {
	vars["run_id"] = runID
	vars["uuid"] = uuid.NewString()
	vars["now"] = time.Now().UTC().Format(time.RFC3339)
}

// resolveAPITarget resolves an api step's target to (app, actor, base_url) via
// the apps map. An empty target is allowed only when exactly one app is declared.
// AppDecl.App defaults to the alias key.
func resolveAPITarget(c *Case, st *APIStep) (app string, actor Actor, baseURL string, err error) {
	target, err := soleOr(st.Target, len(c.Apps), "app", func() string {
		for k := range c.Apps {
			return k
		}
		return ""
	})
	if err != nil {
		return "", Actor{}, "", err
	}
	decl, ok := c.Apps[target]
	if !ok {
		return "", Actor{}, "", errs.Config("TARGET_UNDECLARED", "app target not declared: "+target)
	}
	app = decl.App
	if app == "" {
		app = target
	}
	return app, decl.Actor, decl.BaseURL, nil
}

// resolveTarget resolves a db/log target ref to its target name via the given map.
func resolveTarget(target string, m map[string]string, kind string) (string, error) {
	t, err := soleOr(target, len(m), kind, func() string {
		for k := range m {
			return k
		}
		return ""
	})
	if err != nil {
		return "", err
	}
	name, ok := m[t]
	if !ok {
		return "", errs.Config("TARGET_UNDECLARED", kind+" target not declared: "+t)
	}
	return name, nil
}

// soleOr returns target, or — when target is empty — the sole declared key when
// there is exactly one, else a TARGET_AMBIGUOUS error.
func soleOr(target string, n int, kind string, sole func() string) (string, error) {
	if target != "" {
		return target, nil
	}
	if n == 1 {
		return sole(), nil
	}
	return "", errs.Config("TARGET_AMBIGUOUS",
		fmt.Sprintf("%s target omitted but %d declared; set target:", kind, n))
}

// resolveActorFlags turns an Actor into apicli --actor[/--actor-file] inputs.
// By name -> {Name}; inline -> render vars, hash, write a temp {vars} file.
func (r *Runner) resolveActorFlags(act Actor, vars map[string]string) (resolvedActor, error) {
	if !act.IsSet() {
		return resolvedActor{}, nil
	}
	if act.Name != "" {
		return resolvedActor{Name: act.Name}, nil
	}
	rendered, err := RenderMap(act.Vars, vars)
	if err != nil {
		return resolvedActor{}, err
	}
	h := actorHash(rendered)
	dir, err := r.ensureTmpDir()
	if err != nil {
		return resolvedActor{}, err
	}
	path := filepath.Join(dir, "actor-"+h+".yaml")
	body, err := yaml.Marshal(map[string]any{"vars": rendered})
	if err != nil {
		return resolvedActor{}, errs.General("ACTOR_MARSHAL", err.Error())
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return resolvedActor{}, errs.General("ACTOR_WRITE", err.Error())
	}
	return resolvedActor{Name: "inline-" + h, FilePath: path}, nil
}

func (r *Runner) ensureTmpDir() (string, error) {
	if r.tmpDir != "" {
		return r.tmpDir, nil
	}
	d, err := os.MkdirTemp("", "tcli-actors-")
	if err != nil {
		return "", errs.General("TMPDIR", err.Error())
	}
	r.tmpDir = d
	return d, nil
}
