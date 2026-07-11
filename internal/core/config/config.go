// Package config loads ~/.aidev-clis/<tool>.yaml and resolves the active target.
// AIDEV_CLIS_HOME overrides ~/.aidev-clis. Target precedence: flag > AIDEV_TARGET > default_target.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Target is the resolved target block handed to an adapter.
type Target struct {
	Name    string
	Adapter string         // the backend/adapter name (yaml `adapter`)
	Raw     map[string]any // the full block (adapter reads its own keys)
}

type toolFile struct {
	DefaultTarget string                    `yaml:"default_target"`
	Targets       map[string]map[string]any `yaml:"targets"`
}

// Home returns the config root (AIDEV_CLIS_HOME or ~/.aidev-clis).
func Home() (string, error) {
	if h := os.Getenv("AIDEV_CLIS_HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", errs.Config("HOME_UNKNOWN", err.Error())
	}
	return filepath.Join(h, ".aidev-clis"), nil
}

// loadTool reads and parses <tool>.yaml, returning the parsed file + its path.
func loadTool(tool string) (toolFile, string, error) {
	home, err := Home()
	if err != nil {
		return toolFile{}, "", err
	}
	path := filepath.Join(home, tool+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return toolFile{}, path, errs.Config("CONFIG_MISSING", fmt.Sprintf("cannot read %s: %v", path, err))
	}
	var tf toolFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return toolFile{}, path, errs.Config("CONFIG_INVALID", fmt.Sprintf("%s: %v", path, err))
	}
	return tf, path, nil
}

// TargetInfo is a one-line summary of a configured target for the `targets` discovery
// verb (AI-facing: which target names exist, what backend, and a human label).
type TargetInfo struct {
	Name        string `json:"name"`
	Adapter     string `json:"adapter"`
	Description string `json:"description,omitempty"`
	Scene       string `json:"scene,omitempty"`
}

// ListTargets returns the configured targets of <tool>.yaml sorted by name, each with
// its adapter and optional `description`. Powers `<cli> targets`.
func ListTargets(tool string) ([]TargetInfo, error) {
	tf, _, err := loadTool(tool)
	if err != nil {
		return nil, err
	}
	infos := make([]TargetInfo, 0, len(tf.Targets))
	for name, block := range tf.Targets {
		adapter, _ := block["adapter"].(string)
		desc, _ := block["description"].(string)
		scene, _ := block["scene"].(string)
		infos = append(infos, TargetInfo{Name: name, Adapter: adapter, Description: desc, Scene: scene})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// Resolve loads <tool>.yaml and selects the target (flag > AIDEV_TARGET > default).
func Resolve(tool, flagTarget string) (Target, error) {
	tf, path, err := loadTool(tool)
	if err != nil {
		return Target{}, err
	}
	name := flagTarget
	if name == "" {
		name = os.Getenv("AIDEV_TARGET")
	}
	if name == "" {
		name = tf.DefaultTarget
	}
	if name == "" {
		return Target{}, errs.Config("TARGET_UNSET", "no --target, AIDEV_TARGET, or default_target")
	}
	block, ok := tf.Targets[name]
	if !ok {
		return Target{}, errs.Config("TARGET_NOT_FOUND", fmt.Sprintf("target %q not in %s", name, path))
	}
	adapter, _ := block["adapter"].(string)
	if adapter == "" {
		return Target{}, errs.Config("TARGET_NO_ADAPTER", fmt.Sprintf("target %q has no 'adapter'", name))
	}
	return Target{Name: name, Adapter: adapter, Raw: block}, nil
}

// ResolveForAdapter resolves the active target for an adapter-typed CLI (dbcli,
// logcli). An explicit flagTarget (or AIDEV_TARGET) is honored verbatim — the caller
// still rejects an adapter mismatch. When neither is set, it auto-picks by
// adapter: if exactly one configured target uses `adapter`, that one is used (so a
// single-target driver needs no --target); else `default_target` if it matches the
// adapter; else a clear TARGET_AMBIGUOUS / TARGET_NONE_FOR_ADAPTER error. `adapter`
// must be the canonical adapter name (logcli passes the canonical, not an alias).
func ResolveForAdapter(tool, flagTarget, adapter string) (Target, error) {
	if flagTarget == "" && os.Getenv("AIDEV_TARGET") == "" {
		tf, path, err := loadTool(tool)
		if err != nil {
			return Target{}, err
		}
		var matches []string
		for n, block := range tf.Targets {
			if ad, _ := block["adapter"].(string); ad == adapter {
				matches = append(matches, n)
			}
		}
		sort.Strings(matches)

		defMatches := tf.DefaultTarget != ""
		if defMatches {
			ad, _ := tf.Targets[tf.DefaultTarget]["adapter"].(string)
			defMatches = ad == adapter
		}

		switch {
		case len(matches) == 1:
			return Resolve(tool, matches[0]) // unambiguous: the only target for this adapter
		case defMatches:
			return Resolve(tool, tf.DefaultTarget)
		case len(matches) > 1:
			return Target{}, errs.Config("TARGET_AMBIGUOUS",
				fmt.Sprintf("%d targets use adapter %q (%s); pass --target", len(matches), adapter, strings.Join(matches, ", ")))
		default:
			return Target{}, errs.Config("TARGET_NONE_FOR_ADAPTER",
				fmt.Sprintf("no target with adapter %q in %s", adapter, path))
		}
	}
	return Resolve(tool, flagTarget)
}
