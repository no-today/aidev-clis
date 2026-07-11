package aidev

import (
	"sort"

	"github.com/no-today/aidev-clis/internal/apicli"
	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// targetKeyedTools are the CLIs whose config is keyed by target name (joined by name
// across files). apicli is app-keyed and handled separately.
var targetKeyedTools = []string{"dbcli", "jcli", "logcli"}

// Capability is the set of CLIs usable for a target.
type Capability struct {
	CLIs []string `json:"clis"`
}

// Workspace describes the resolved scope. Scene is nil when no scene is active.
type Workspace struct {
	Scene  *string `json:"scene"`
	Source string  `json:"source"`
}

// Inventory is the discovery payload (the {data} body).
type Inventory struct {
	Workspace Workspace             `json:"workspace"`
	Tools     []string              `json:"tools"`
	Targets   map[string]Capability `json:"targets"`
	Apps      []string              `json:"apps"`
	Notes     []string              `json:"notes,omitempty"`
}

func inScope(active string, sceneSet map[string]bool) bool {
	if active == "" {
		return true // no scoping
	}
	if len(sceneSet) == 0 {
		return true // global (untagged)
	}
	return sceneSet[active]
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCapKeys(m map[string]Capability) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isMissing(err error) bool {
	e, ok := err.(*errs.Error)
	return ok && e.Code == "CONFIG_MISSING"
}

// Build assembles the workspace-scoped inventory for the given scene.
func Build(scene Scene) (Inventory, error) {
	inv := Inventory{
		Tools:   []string{},
		Targets: map[string]Capability{},
		Apps:    []string{},
	}
	if scene.Name == "" {
		inv.Workspace = Workspace{Scene: nil, Source: scene.Source}
	} else {
		n := scene.Name
		inv.Workspace = Workspace{Scene: &n, Source: scene.Source}
	}

	type agg struct {
		tools    map[string]bool
		sceneSet map[string]bool
	}
	targets := map[string]*agg{}

	for _, tool := range targetKeyedTools {
		infos, err := config.ListTargets(tool)
		if err != nil {
			if isMissing(err) {
				continue // tool not configured here — skip silently
			}
			inv.Notes = append(inv.Notes, tool+".yaml: "+errs.From(err).Message)
			continue
		}
		for _, info := range infos {
			a := targets[info.Name]
			if a == nil {
				a = &agg{tools: map[string]bool{}, sceneSet: map[string]bool{}}
				targets[info.Name] = a
			}
			a.tools[tool] = true
			if info.Scene != "" {
				a.sceneSet[info.Scene] = true
			}
		}
	}

	toolSet := map[string]bool{}
	for name, a := range targets {
		if !inScope(scene.Name, a.sceneSet) {
			continue
		}
		clis := sortedKeys(a.tools)
		inv.Targets[name] = Capability{CLIs: clis}
		for _, c := range clis {
			toolSet[c] = true
		}
	}

	// apicli is app-keyed: each app is its own target.
	if cfg, err := apicli.LoadConfig(); err != nil {
		if !isMissing(err) {
			inv.Notes = append(inv.Notes, "apicli.yaml: "+errs.From(err).Message)
		}
	} else {
		for appName, app := range cfg.Apps {
			set := map[string]bool{}
			if app.Scene != "" {
				set[app.Scene] = true
			}
			if inScope(scene.Name, set) {
				inv.Apps = append(inv.Apps, appName)
				toolSet["apicli"] = true
			}
		}
	}
	sort.Strings(inv.Apps)

	inv.Tools = sortedKeys(toolSet)
	return inv, nil
}
