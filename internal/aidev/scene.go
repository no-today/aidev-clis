// Package aidev is the cross-CLI discovery aggregator: it resolves the active
// workspace scene and joins the per-tool configs into one inventory. It is the
// one place allowed to read every CLI's config; it never dispatches to them.
package aidev

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Scene is the resolved workspace scope. Name == "" means no scoping (every
// env/app is in scope). Source records where it came from: a .aidev.yaml path,
// "AIDEV_SCENE", or "none".
type Scene struct {
	Name   string
	Source string
}

type aidevMarker struct {
	Scene string `yaml:"scene"`
}

// ResolveScene determines the active scene. Precedence: AIDEV_SCENE env >
// nearest .aidev.yaml walking up from startDir > none.
func ResolveScene(startDir string) Scene {
	if s := os.Getenv("AIDEV_SCENE"); s != "" {
		return Scene{Name: s, Source: "AIDEV_SCENE"}
	}
	dir := startDir
	for {
		p := filepath.Join(dir, ".aidev.yaml")
		if data, err := os.ReadFile(p); err == nil {
			var m aidevMarker
			_ = yaml.Unmarshal(data, &m)
			return Scene{Name: m.Scene, Source: p}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Scene{Name: "", Source: "none"}
		}
		dir = parent
	}
}
