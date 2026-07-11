package apicli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Actors is the parsed actors.yaml: app -> actor -> vars. Missing file is OK
// (an app may only ever use --actor with inline-configured defaults elsewhere).
type Actors struct {
	Actors map[string]map[string]map[string]string `yaml:"actors"`
}

// Vars returns the var map for (app, actor), or nil if absent.
func (a *Actors) Vars(app, actor string) map[string]string {
	if a == nil {
		return nil
	}
	return a.Actors[app][actor]
}

// LoadActors reads ~/.aidev-clis/actors.yaml. A missing file yields an empty set.
func LoadActors() (*Actors, error) {
	home, err := config.Home()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, "actors.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Actors{Actors: map[string]map[string]map[string]string{}}, nil
	}
	if err != nil {
		return nil, errs.Config("CONFIG_MISSING", fmt.Sprintf("cannot read %s: %v", path, err))
	}
	var a Actors
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, errs.Config("CONFIG_INVALID", fmt.Sprintf("%s: %v", path, err))
	}
	return &a, nil
}

// InlineActor is the shape of an --actor-file: a complete one-off account.
type InlineActor struct {
	Vars map[string]string `yaml:"vars"`
}

// LoadInlineActor reads a one-off actor file ({vars: {...}}). The returned vars
// are complete and do NOT merge with any actors.yaml account.
func LoadInlineActor(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.Config("ACTOR_FILE_MISSING", fmt.Sprintf("cannot read %s: %v", path, err))
	}
	var a InlineActor
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, errs.Config("ACTOR_FILE_INVALID", fmt.Sprintf("%s: %v", path, err))
	}
	return a.Vars, nil
}
