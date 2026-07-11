package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/apicli"
	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// commonFlags are the shared addressing flags on every verb.
type commonFlags struct {
	actor, env, baseURL, actorFile string
}

func (f *commonFlags) bind(c *cobra.Command) {
	c.Flags().StringVar(&f.actor, "actor", "", "account (default: app default_actor)")
	c.Flags().StringVar(&f.env, "env", "", "named environment of this app")
	c.Flags().StringVar(&f.baseURL, "base-url", "", "ad-hoc base URL override")
	c.Flags().StringVar(&f.actorFile, "actor-file", "", "inline one-off actor file ({vars:{...}})")
}

func (f *commonFlags) resolve(app string) (*apicli.Target, error) {
	cfg, err := apicli.LoadConfig()
	if err != nil {
		return nil, err
	}
	actors, err := apicli.LoadActors()
	if err != nil {
		return nil, err
	}
	tg, err := apicli.Resolve(cfg, actors, apicli.Selector{
		App: app, Actor: f.actor, Env: f.env, BaseURL: f.baseURL,
	})
	if err != nil {
		return nil, err
	}
	if f.actorFile != "" {
		vars, ferr := apicli.LoadInlineActor(f.actorFile)
		if ferr != nil {
			return nil, ferr
		}
		expanded, eerr := apicli.ExpandSecrets(vars)
		if eerr != nil {
			return nil, eerr
		}
		tg.Vars = expanded
	}
	dg.Logf(1, "resolved app=%s actor=%s env=%s base_url=%s", tg.App, tg.Actor, tg.Env, tg.BaseURL)
	return tg, nil
}

// callCmd: apicli call <app> <path> [-X .. -H .. -d ..]
func callCmd() *cobra.Command {
	var f commonFlags
	var method, data, output string
	var outputFile, headersFile string
	var headers []string
	var timeout time.Duration
	var curl bool
	var allowCrossOrigin bool
	c := &cobra.Command{
		Use:   "call <app> <path>",
		Short: "send an HTTP request using the active session",
		Args:  appArgs(2, "<path>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "json" && output != "raw" {
				e := errs.Config("UNSUPPORTED_OUTPUT", "apicli supports --output json|raw")
				beginAudit(args[0], "", false, nil).Finish(e, nil)
				return e
			}
			err := func() error {
				app, path := args[0], args[1]
				if err := validatePath(path); err != nil {
					beginAudit(app, "", false, nil).Finish(err, nil)
					return err
				}
				tg, err := f.resolve(app)
				if err != nil {
					beginAudit(app, "", false, nil).Finish(err, nil)
					return err
				}
				method = strings.ToUpper(method)
				if curl {
					// Preview only — never hits the backend, so not side-effecting.
					beginAudit(tg.App, tg.Env, false, nil).Finish(nil, nil)
					_, _ = os.Stdout.WriteString(apicli.ToCurl(tg, &apicli.CallRequest{
						Method: method, Path: path, Headers: headers, Body: []byte(data),
					}) + "\n")
					return nil
				}
				req := &apicli.CallRequest{
					Method: method, Path: path, Headers: headers,
					Body: []byte(data), Timeout: timeout,
					OutputFile: outputFile, HeadersFile: headersFile,
					AllowCrossOrigin: allowCrossOrigin,
				}
				sideEffecting := method != "GET" && method != "HEAD"
				reqMap := map[string]any{
					"method": method,
					"url":    apicli.ResolveURL(tg, path),
					"actor":  tg.Actor,
				}
				if h := headerMap(headers); len(h) > 0 {
					reqMap["headers"] = h
				}
				op := beginAudit(tg.App, tg.Env, sideEffecting, reqMap)
				res, err := apicli.Call(tg, req)
				if err != nil {
					op.Finish(err, nil)
					return err
				}
				dg.Logf(1, "%s %s → status=%d ok=%v", method, path, res.Status, res.OK)
				if res.Relogged {
					dg.Logf(1, "session re-established")
				}
				if !res.OK && res.TraceID != "" {
					// New logcli is adapter-first; trace is an aliyun-sls verb.
					hint := "[apicli] traceId: " + res.TraceID + " → logcli sls trace " + res.TraceID
					if tg.LogEnv != "" {
						hint += " --target " + tg.LogEnv
					}
					_, _ = os.Stderr.WriteString(hint + "\n")
				}
				if !res.OK {
					businessExit = 1
				}
				op.Finish(nil, map[string]any{"status": res.Status})
				if output == "raw" {
					_, werr := os.Stdout.WriteString(toString(res.Body))
					return werr
				}
				var warns []string
				if res.Relogged {
					warns = append(warns, "session re-established")
				}
				return emitWarn(res, warns)
			}()
			if err != nil && output == "raw" {
				e := errs.From(err)
				_ = envelope.WriteRawError(os.Stdout, e.Code, e.Message)
				os.Exit(e.Exit)
			}
			return err
		},
	}
	f.bind(c)
	c.Flags().StringVarP(&method, "request", "X", "GET", "HTTP method")
	c.Flags().StringArrayVarP(&headers, "header", "H", nil, "request header (repeatable)")
	c.Flags().StringVarP(&data, "data", "d", "", "request body (raw, passthrough)")
	c.Flags().StringVar(&output, "output", "json", "json|raw")
	_ = c.RegisterFlagCompletionFunc("output", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "raw"}, cobra.ShellCompDirectiveNoFileComp
	})
	c.Flags().StringVar(&outputFile, "output-file", "", "stream response body to this file (binary-safe)")
	c.Flags().StringVar(&headersFile, "headers-file", "", "write status/url/headers JSON to this file")
	c.Flags().DurationVar(&timeout, "connect-timeout", 0, "overall request timeout, connect + response (default 30s)")
	c.Flags().BoolVar(&curl, "curl", false, "print equivalent curl, don't execute")
	c.Flags().BoolVar(&allowCrossOrigin, "allow-cross-origin", false, "permit sending the session to a host other than the app base")
	return c
}

func loginCmd() *cobra.Command {
	var f commonFlags
	var vars []string
	c := &cobra.Command{
		Use: "login <app>", Short: "authenticate and capture a session", Args: appArgs(1, ""),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			tg, err := f.resolve(app)
			if err != nil {
				beginAudit(app, "", false, nil).Finish(err, nil)
				return err
			}
			// --var is written into tg.Vars AFTER actor resolution, so it
			// intentionally overrides an actor var of the same name (precedence
			// actor < --var). Login then layers captured vars on top of tg.Vars.
			for _, kv := range vars {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					e := errs.Config("VAR_INVALID", "--var must be key=value: "+kv)
					beginAudit(tg.App, tg.Env, false, nil).Finish(e, nil)
					return e
				}
				if tg.Vars == nil {
					tg.Vars = map[string]string{}
				}
				tg.Vars[k] = v
			}
			sess, err := apicli.Login(tg)
			if err != nil {
				beginAudit(tg.App, tg.Env, false, nil).Finish(err, nil)
				return err
			}
			if err := apicli.SaveSession(tg, sess); err != nil {
				beginAudit(tg.App, tg.Env, false, nil).Finish(err, nil)
				return err
			}
			beginAudit(tg.App, tg.Env, false, nil).Finish(nil, nil)
			payload := map[string]any{"app": app, "actor": tg.Actor, "status": "logged-in"}
			if len(vars) > 0 {
				return emitWarn(payload, []string{"--var values are visible in process argv and shell history"})
			}
			return emit(payload)
		},
	}
	f.bind(c)
	c.Flags().StringArrayVar(&vars, "var", nil, "per-invocation login value key=value (e.g. verifyCode=9999), repeatable")
	return c
}

func whoamiCmd() *cobra.Command {
	var f commonFlags
	c := &cobra.Command{
		Use: "whoami <app>", Short: "inspect the current session (local)", Args: appArgs(1, ""),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			tg, err := f.resolve(app)
			if err != nil {
				beginAudit(app, "", false, nil).Finish(err, nil)
				return err
			}
			sess, ok := apicli.LoadSession(tg)
			beginAudit(tg.App, tg.Env, false, nil).Finish(nil, nil)
			out := map[string]any{"app": app, "actor": tg.Actor, "logged_in": ok}
			if age, has := apicli.SessionAgeSeconds(tg); has {
				out["age_seconds"] = age
				out["expiry_risk"] = expiryRisk(age)
			}
			if ok && len(sess.Vars) > 0 {
				keys := make([]string, 0, len(sess.Vars))
				for k := range sess.Vars {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				out["captured_vars"] = keys
			}
			return emit(out)
		},
	}
	f.bind(c)
	return c
}

func logoutCmd() *cobra.Command {
	var f commonFlags
	c := &cobra.Command{
		Use: "logout <app>", Short: "remove the stored session", Args: appArgs(1, ""),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			tg, err := f.resolve(app)
			if err != nil {
				beginAudit(app, "", false, nil).Finish(err, nil)
				return err
			}
			if err := apicli.DeleteSession(tg); err != nil {
				beginAudit(tg.App, tg.Env, false, nil).Finish(err, nil)
				return err
			}
			beginAudit(tg.App, tg.Env, false, nil).Finish(nil, nil)
			return emit(map[string]any{"app": app, "status": "logged-out"})
		},
	}
	f.bind(c)
	return c
}

// headerMap parses -H "K: V" pairs into a map, dropping Cookie (the injected
// session) so a live credential never lands in the audit log.
func headerMap(headers []string) map[string]string {
	out := map[string]string{}
	for _, h := range headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if strings.EqualFold(k, "Cookie") {
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// expiryRisk buckets a session age: low <=8h, med <=24h, high otherwise.
func expiryRisk(ageSeconds int64) string {
	switch {
	case ageSeconds <= 8*3600:
		return "low"
	case ageSeconds <= 24*3600:
		return "med"
	default:
		return "high"
	}
}

// httpVerbs are the methods an agent plausibly pastes in front of a path.
var httpVerbs = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// validatePath rejects the two observed silent-failure shapes: a method verb
// pasted into the path ("POST /x" would be requested literally as that URL and
// return a 200 HTML page — no error, wrong result) and raw whitespace (invalid
// in a URL; almost always a quoting slip).
func validatePath(path string) error {
	if verb, rest, ok := strings.Cut(path, " "); ok && httpVerbs[strings.ToUpper(verb)] {
		return errs.Config("PATH_HAS_METHOD",
			fmt.Sprintf("path starts with %q — pass the method as -X %s and the path %q separately",
				verb, strings.ToUpper(verb), rest))
	}
	if strings.ContainsAny(path, " \t") {
		return errs.Config("PATH_HAS_SPACE", "path contains whitespace — URL-encode it (%20) or fix the quoting")
	}
	return nil
}

// appArgs is ExactArgs with an error that names the missing piece and points at
// discovery, instead of cobra's bare "accepts N arg(s)".
func appArgs(n int, extra string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		want := "<app>"
		if extra != "" {
			want += " " + extra
		}
		if len(args) < n {
			return errs.Config("MISSING_ARG",
				fmt.Sprintf("expected %s (got %d arg(s)); run `apicli apps` to list configured apps", want, len(args)))
		}
		return errs.Config("TOO_MANY_ARGS",
			fmt.Sprintf("expected exactly %s (got %d args) — quote the path/body if it contains spaces", want, len(args)))
	}
}

// appsCmd: `apicli apps` — read-only discovery of configured apps + actors, so
// an agent never has to grep ~/.aidev-clis/*.yaml to learn what exists.
func appsCmd() *cobra.Command {
	return &cobra.Command{
		Use: "apps", Short: "list configured apps (base_url, envs, actors)", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			op := audit.Begin(audit.Record{Tool: "apicli", Command: audit.CommandLine(os.Args)})
			cfg, err := apicli.LoadConfig()
			if err != nil {
				op.Finish(err, nil)
				return err
			}
			actors, err := apicli.LoadActors()
			if err != nil {
				op.Finish(err, nil)
				return err
			}
			type appInfo struct {
				App          string   `json:"app"`
				BaseURL      string   `json:"base_url"`
				Envs         []string `json:"envs,omitempty"`
				DefaultActor string   `json:"default_actor,omitempty"`
				Actors       []string `json:"actors,omitempty"`
				AuthKind     string   `json:"auth_kind,omitempty"`
			}
			names := make([]string, 0, len(cfg.Apps))
			for name := range cfg.Apps {
				names = append(names, name)
			}
			sort.Strings(names)
			out := make([]appInfo, 0, len(names))
			for _, name := range names {
				app := cfg.Apps[name]
				info := appInfo{App: name, BaseURL: app.BaseURL, DefaultActor: app.DefaultActor, AuthKind: app.Auth.Kind}
				for e := range app.Envs {
					info.Envs = append(info.Envs, e)
				}
				sort.Strings(info.Envs)
				for a := range actors.Actors[name] {
					info.Actors = append(info.Actors, a)
				}
				sort.Strings(info.Actors)
				out = append(out, info)
			}
			op.Finish(nil, nil)
			return emit(out)
		},
	}
}
