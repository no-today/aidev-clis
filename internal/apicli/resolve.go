package apicli

import (
	"fmt"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/cred"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Selector is the raw addressing input from the command line.
type Selector struct {
	App     string
	Actor   string // empty -> app.DefaultActor
	Env     string // empty -> app default base_url; else key into app.Envs
	BaseURL string // empty -> use Env; mutually exclusive with Env
}

// Target is a fully resolved call target.
type Target struct {
	App                string
	Actor              string
	Env                string // "" means the app's default env
	BaseURL            string
	InsecureSkipVerify bool
	CACert             string
	Vars               map[string]string
	Auth               Auth
	Response           Response
	ExtraHeaders       map[string]string
	TraceField         string
	LogEnv             string
}

// Resolve applies the 3-axis rules: app (required) -> actor (default) ->
// env|base-url (mutually exclusive override).
func Resolve(cfg *Config, actors *Actors, sel Selector) (*Target, error) {
	app, ok := cfg.Apps[sel.App]
	if sel.App == "" || !ok {
		return nil, errs.Config("APP_NOT_FOUND", fmt.Sprintf("app %q not in apicli.yaml", sel.App))
	}
	if sel.Env != "" && sel.BaseURL != "" {
		return nil, errs.Config("ADDRESS_CONFLICT", "--env and --base-url are mutually exclusive")
	}
	actor := sel.Actor
	if actor == "" {
		actor = app.DefaultActor
	}
	base := app.BaseURL
	switch {
	case sel.BaseURL != "":
		base = sel.BaseURL
	case sel.Env != "":
		b, ok := app.Envs[sel.Env]
		if !ok {
			return nil, errs.Config("ENV_NOT_FOUND", fmt.Sprintf("env %q not in app %q", sel.Env, sel.App))
		}
		base = b
	}
	if base == "" {
		return nil, errs.Config("CONFIG_INVALID", fmt.Sprintf("app %q has no base_url", sel.App))
	}
	for _, p := range []struct{ field, val string }{
		{"app", sel.App}, {"actor", actor}, {"env", sel.Env},
	} {
		if !validName(p.val) {
			return nil, errs.Config("INVALID_SELECTOR", fmt.Sprintf("invalid %s %q: must not contain '/', '\\', or '..'", p.field, p.val))
		}
	}
	vars, err := ExpandSecrets(actors.Vars(sel.App, actor))
	if err != nil {
		return nil, err
	}
	return &Target{
		App: sel.App, Actor: actor, Env: sel.Env, BaseURL: base,
		InsecureSkipVerify: app.InsecureSkipVerify, CACert: app.CACert,
		Vars: vars, Auth: app.Auth, Response: app.Response,
		ExtraHeaders: app.ExtraHeaders, TraceField: app.TraceField,
		LogEnv: app.LogEnv,
	}, nil
}

// ExpandSecrets replaces any "secret:<name>" var value with the credential's
// content (trimmed of a trailing newline) read via the perm-checked cred store.
// Plain values pass through unchanged.
func ExpandSecrets(vars map[string]string) (map[string]string, error) {
	if vars == nil {
		return nil, nil
	}
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		if name, ok := strings.CutPrefix(v, "secret:"); ok {
			b, err := cred.Read(name)
			if err != nil {
				return nil, err
			}
			out[k] = strings.TrimRight(string(b), "\r\n")
			continue
		}
		out[k] = v
	}
	return out, nil
}

// validName rejects values that would escape or break the session path layout.
// Empty is allowed (callers treat empty actor/env as "use default"); a non-empty
// value must not contain path separators or "..".
func validName(v string) bool {
	return !strings.ContainsAny(v, `/\`) && !strings.Contains(v, "..")
}
