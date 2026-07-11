package jcli

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// tokenRe matches ${name} (name = letters/digits/_/.). $$ is handled separately.
var tokenRe = regexp.MustCompile(`\$\{([A-Za-z0-9_.]+)\}`)

// Templater resolves ${...} tokens for a deploy invocation. Reserved names
// (service, vars.*, extract names) take precedence over bare params.
type Templater struct {
	Service   string
	Params    map[string]string
	Vars      map[string]string
	Extracts  map[string]string
	Artifacts []Artifact
}

func (t *Templater) lookup(name string) (string, bool) {
	switch {
	case name == "service":
		return t.Service, true
	case strings.HasPrefix(name, "vars."):
		v, ok := t.Vars[name[len("vars."):]]
		return v, ok
	case strings.HasPrefix(name, "param."):
		v, ok := t.Params[name[len("param."):]]
		return v, ok
	case strings.HasPrefix(name, "artifacts."):
		return t.artifact(name[len("artifacts."):])
	}
	if v, ok := t.Extracts[name]; ok {
		return v, true
	}
	if v, ok := t.Params[name]; ok {
		return v, true
	}
	return "", false
}

// artifact resolves "N" or "N.field" against the Artifacts list. Bare index =
// fileName. Out-of-range index or unknown field → not found (TEMPLATE_UNRESOLVED).
func (t *Templater) artifact(ref string) (string, bool) {
	idxStr, field, hasField := strings.Cut(ref, ".")
	i, err := strconv.Atoi(idxStr)
	if err != nil || i < 0 || i >= len(t.Artifacts) {
		return "", false
	}
	a := t.Artifacts[i]
	switch {
	case !hasField, field == "fileName":
		return a.FileName, true
	case field == "relativePath":
		return a.RelativePath, true
	}
	return "", false
}

// Expand substitutes ${...} tokens. `$$` is a literal `$` (so a bash step can use
// the shell's own ${VAR} as $${VAR}). An unknown ${...} → TEMPLATE_UNRESOLVED.
func (t *Templater) Expand(s string) (string, error) { return t.expand(s, false) }

// ExpandRegex is Expand but QuoteMeta's each substituted value (regex context).
func (t *Templater) ExpandRegex(s string) (string, error) { return t.expand(s, true) }

func (t *Templater) expand(s string, quoteMeta bool) (string, error) {
	// Split on `$$` so each literal `$` is preserved on rejoin; tokens are only
	// substituted within the segments.
	parts := strings.Split(s, "$$")
	var bad string
	for i, part := range parts {
		parts[i] = tokenRe.ReplaceAllStringFunc(part, func(m string) string {
			name := tokenRe.FindStringSubmatch(m)[1]
			v, ok := t.lookup(name)
			if !ok {
				bad = name
				return m
			}
			if quoteMeta {
				return regexp.QuoteMeta(v)
			}
			return v
		})
	}
	if bad != "" {
		return "", errs.Config("TEMPLATE_UNRESOLVED", "unknown template token ${"+bad+"}")
	}
	return strings.Join(parts, "$"), nil
}
