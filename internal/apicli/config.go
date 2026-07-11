// Package apicli is the apicli core: app/actor config, sessions, the HTTP call,
// login flows, and the per-app response predicate engine. apicli addresses
// "apps" (each with its own base URL + auth), NOT the shared --env model.
package apicli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Config is the parsed apicli.yaml.
type Config struct {
	Apps map[string]*App `yaml:"apps"`
}

// App is one configured 端: base URL + named envs + auth + response rules.
type App struct {
	BaseURL            string            `yaml:"base_url"`
	InsecureSkipVerify bool              `yaml:"insecure_skip_verify"`
	CACert             string            `yaml:"ca_cert"`
	Envs               map[string]string `yaml:"envs"`
	DefaultActor       string            `yaml:"default_actor"`
	Auth               Auth              `yaml:"auth"`
	Response           Response          `yaml:"response"`
	ExtraHeaders       map[string]string `yaml:"extra_headers"`
	TraceField         string            `yaml:"trace_field"`
	LogEnv             string            `yaml:"log_env"`
	Scene              string            `yaml:"scene"` // optional; consumed only by `aidev` discovery
}

// Auth declares how to (re)establish a session for an app.
type Auth struct {
	Kind         string            `yaml:"kind"` // flow | none (cookie replay is per-step cookie_from_set_cookie, not a kind)
	VarsDefaults map[string]string `yaml:"vars_defaults"`
	VarsRequired []string          `yaml:"vars_required"`
	Flow         []FlowStep        `yaml:"flow"`
	Inject       AuthInject        `yaml:"inject"`
}

// FlowStep is one raw-HTTP request in a login flow plus the values to capture.
type FlowStep struct {
	Name    string            `yaml:"name"`    // optional; shown in a failed-assert error
	Request string            `yaml:"request"` // raw HTTP: "METHOD PATH\nHeader: v\n\nbody"
	Assert  string            `yaml:"assert"`  // predicate that must hold post-step, else AUTH_FAILED
	Capture map[string]string `yaml:"capture"` // var -> gjson path over the body

	CookieFromSetCookie bool `yaml:"cookie_from_set_cookie"`
}

// AuthInject says how a captured session rides on each call.
type AuthInject struct {
	Header  string            `yaml:"header"`  // e.g. "Authorization: Bearer {{token}}"
	Headers map[string]string `yaml:"headers"` // name -> template (rendered from session vars)
	Cookie  string            `yaml:"cookie"`  // e.g. "SESSION={{token}}"
}

// Response holds the per-app predicates (see predicate.go).
type Response struct {
	OKWhen      string `yaml:"ok_when"`
	ExpiredWhen string `yaml:"expired_when"`
}

// LoadConfig reads and parses ~/.aidev-clis/apicli.yaml.
func LoadConfig() (*Config, error) {
	home, err := config.Home()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, "apicli.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.Config("CONFIG_MISSING", fmt.Sprintf("cannot read %s: %v", path, err))
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, errs.Config("CONFIG_INVALID", fmt.Sprintf("%s: %v", path, err))
	}
	if len(c.Apps) == 0 {
		return nil, errs.Config("CONFIG_INVALID", "apicli.yaml has no apps")
	}
	return &c, nil
}
