package tcli

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Case is the top-level test case parsed from YAML (v2 schema).
type Case struct {
	Name       string             `yaml:"name"`
	Tags       []string           `yaml:"tags"`
	Vars       map[string]string  `yaml:"vars"`
	Apps       map[string]AppDecl `yaml:"apps"` // alias -> app identity
	DBs        map[string]string  `yaml:"dbs"`  // alias -> dbcli env
	Logs       map[string]string  `yaml:"logs"` // alias -> logcli env
	Safety     Safety             `yaml:"safety"`
	Setup      []Action           `yaml:"setup"`
	Steps      []Action           `yaml:"steps"`
	Assertions []Action           `yaml:"assertions"`
	Cleanup    []Action           `yaml:"cleanup"`
	Path       string             `yaml:"-"`
}

// AppDecl is one entry of the apps map. App defaults to the map key.
type AppDecl struct {
	App     string `yaml:"app"`
	Actor   Actor  `yaml:"actor"`
	BaseURL string `yaml:"base_url"`
}

type Safety struct {
	AllowDBWrite bool `yaml:"allow_db_write"`
}

// Actor: string (by name) or {vars:{...}} (inline). Custom UnmarshalYAML.
type Actor struct {
	Name string
	Vars map[string]string
}

// Action is one step. Exactly one of API/DB/Log; or, with none set, a standalone
// assertion action carrying only Expect (expressions over captured vars).
type Action struct {
	Name   string   `yaml:"name"`
	API    *APIStep `yaml:"api"`
	DB     *DBStep  `yaml:"db"`
	Log    *LogStep `yaml:"log"`
	Expect []string `yaml:"expect"` // standalone assertion when API/DB/Log are nil
}

type APIStep struct {
	Target   string            `yaml:"target"` // alias into apps; optional if one app
	Request  string            `yaml:"request"`
	SaveBody string            `yaml:"save_body"`
	Expect   []string          `yaml:"expect"`
	Capture  map[string]string `yaml:"capture"`
	Wait     *Wait             `yaml:"wait"`
	Timeout  string            `yaml:"timeout"`
	Assert   *ScriptAssert     `yaml:"assert_script"`
}
type DBStep struct {
	Target  string            `yaml:"target"`
	SQL     string            `yaml:"sql"`
	Write   bool              `yaml:"write"`
	Expect  []string          `yaml:"expect"`
	Capture map[string]string `yaml:"capture"`
	Wait    *Wait             `yaml:"wait"`
	Timeout string            `yaml:"timeout"`
}
type LogStep struct {
	Target  string            `yaml:"target"`
	Trace   string            `yaml:"trace"`
	Query   string            `yaml:"query"`
	From    string            `yaml:"from"`
	To      string            `yaml:"to"`
	Size    int               `yaml:"size"`
	Expect  []string          `yaml:"expect"`
	Capture map[string]string `yaml:"capture"`
	Wait    *Wait             `yaml:"wait"`
	Timeout string            `yaml:"timeout"`
}

type Wait struct {
	Timeout  string `yaml:"timeout"`
	Interval string `yaml:"interval"`
}
type ScriptAssert struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Timeout string   `yaml:"timeout"`
}

// UnmarshalYAML supports actor written as a scalar (by name) or a mapping
// {vars:{...}} (inline).
func (a *Actor) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		a.Name = node.Value
		return nil
	}
	var inline struct {
		Vars map[string]string `yaml:"vars"`
	}
	if err := node.Decode(&inline); err != nil {
		return err
	}
	a.Vars = inline.Vars
	return nil
}

func (a Actor) IsInline() bool { return len(a.Vars) > 0 }
func (a Actor) IsSet() bool    { return a.Name != "" || len(a.Vars) > 0 }

// ParseCase reads and parses a case file, filling Path.
func ParseCase(path string) (*Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.Config("CASE_READ", err.Error())
	}
	return parseCaseBytes(data, path)
}

func parseCaseBytes(data []byte, path string) (*Case, error) {
	var c Case
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, errs.Config("CASE_INVALID", fmt.Sprintf("%s: %v", path, err))
	}
	c.Path = path
	return &c, nil
}

// actionKind returns the sole action type, or "" when it is not exactly one.
// A standalone assertion (only Expect set) has kind "expect".
func (a Action) actionKind() string {
	var kinds []string
	if a.API != nil {
		kinds = append(kinds, "api")
	}
	if a.DB != nil {
		kinds = append(kinds, "db")
	}
	if a.Log != nil {
		kinds = append(kinds, "log")
	}
	if len(kinds) == 0 && len(a.Expect) > 0 {
		return "expect"
	}
	if len(kinds) != 1 {
		return ""
	}
	return kinds[0]
}

// stepExpect returns the expression list of whichever backend step is set.
func (a Action) stepExpect() []string {
	switch {
	case a.API != nil:
		return a.API.Expect
	case a.DB != nil:
		return a.DB.Expect
	case a.Log != nil:
		return a.Log.Expect
	default:
		return a.Expect
	}
}

// Validate returns human-readable diagnostics (empty = valid). Pure/static.
func (c *Case) Validate() []string {
	var d []string
	if c.Name == "" {
		d = append(d, "case has no name")
	}
	// resolve a target against a declared map: "" is OK only when single-entry.
	checkTarget := func(phase string, i int, a Action, kind, target string, decls int) {
		if target == "" {
			if decls != 1 {
				d = append(d, fmt.Sprintf("%s[%d] %q: %s target omitted but %d %ss declared (declare one, or set target)", phase, i, a.Name, kind, decls, kind))
			}
			return
		}
		ok := false
		switch kind {
		case "app":
			_, ok = c.Apps[target]
		case "db":
			_, ok = c.DBs[target]
		case "log":
			_, ok = c.Logs[target]
		}
		if !ok {
			d = append(d, fmt.Sprintf("%s[%d] %q: %s target %q not declared (add it to %ss:)", phase, i, a.Name, kind, target, kind))
		}
	}
	checkExprs := func(phase string, i int, a Action) {
		for _, e := range a.stepExpect() {
			// {{vars}} are unknown statically; check the operator structure only
			// when the expression has no template (a templated one is checked at run).
			if strings.Contains(e, "{{") {
				continue
			}
			if _, err := ParseExpr(e); err != nil {
				d = append(d, fmt.Sprintf("%s[%d] %q: bad expect %q: %s", phase, i, a.Name, e, errs.From(err).Message))
			}
		}
	}
	checkPhase := func(phase string, acts []Action) {
		for i, a := range acts {
			kind := a.actionKind()
			if kind == "" {
				d = append(d, fmt.Sprintf("%s[%d] %q: must have exactly one of api/db/log (or only expect:)", phase, i, a.Name))
				continue
			}
			switch kind {
			case "api":
				checkTarget(phase, i, a, "app", a.API.Target, len(c.Apps))
			case "db":
				checkTarget(phase, i, a, "db", a.DB.Target, len(c.DBs))
				if a.DB.Write && !c.Safety.AllowDBWrite {
					d = append(d, fmt.Sprintf("%s[%d] %q: db write requires safety.allow_db_write: true", phase, i, a.Name))
				}
			case "log":
				checkTarget(phase, i, a, "log", a.Log.Target, len(c.Logs))
			}
			checkExprs(phase, i, a)
		}
	}
	checkPhase("setup", c.Setup)
	checkPhase("steps", c.Steps)
	checkPhase("assertions", c.Assertions)
	checkPhase("cleanup", c.Cleanup)
	d = append(d, c.checkVars()...)
	return d
}

// TagMatch reports whether c has every tag in want (AND, case-insensitive).
func (c *Case) TagMatch(want []string) bool {
	if len(want) == 0 {
		return true
	}
	have := map[string]bool{}
	for _, t := range c.Tags {
		have[strings.ToLower(t)] = true
	}
	for _, w := range want {
		if !have[strings.ToLower(w)] {
			return false
		}
	}
	return true
}
