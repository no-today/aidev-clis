package jcli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// CachedJob is one buildable Jenkins job (identity only — nothing that goes stale).
type CachedJob struct {
	Name string `json:"name"` // leaf name (what you type / completion source)
	Path string `json:"path"` // /-separated full path (--job / display / fallback)
	URL  string `json:"url"`  // convenience; derivable from base_url + path
}

// JobsCache is the on-disk jobs.json for one target.
type JobsCache struct {
	Target   string      `json:"target"`
	BaseURL  string      `json:"base_url"`
	SyncedAt string      `json:"synced_at"`
	Jobs     []CachedJob `json:"jobs"`
}

// cachePath returns ~/.aidev-clis/cache/jcli/<target>/jobs.json.
func cachePath(target string) (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "cache", "jcli", target, "jobs.json"), nil
}

// Save writes the cache (0700 dir, 0644 file).
func (c *JobsCache) Save() error {
	p, err := cachePath(c.Target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return errs.Config("CACHE_DIR", err.Error())
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return errs.General("CACHE_ENCODE", err.Error())
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return errs.Config("CACHE_WRITE", err.Error())
	}
	return nil
}

// LoadJobsCache reads the cache for target (CACHE_MISSING if never synced).
func LoadJobsCache(target string) (*JobsCache, error) {
	p, err := cachePath(target)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, errs.Config("CACHE_MISSING", "no jobs cache for target "+target+"; run `jcli jobs --sync --target "+target+"`")
	}
	var c JobsCache
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, errs.Config("CACHE_INVALID", err.Error())
	}
	return &c, nil
}

// FindByName returns the cached jobs whose leaf name == name (0, 1, or many).
func (c *JobsCache) FindByName(name string) []CachedJob {
	var out []CachedJob
	for _, j := range c.Jobs {
		if j.Name == name {
			out = append(out, j)
		}
	}
	return out
}

// HasPath reports whether a cached job has exactly this /-separated path.
func (c *JobsCache) HasPath(path string) bool {
	for _, j := range c.Jobs {
		if j.Path == path {
			return true
		}
	}
	return false
}
