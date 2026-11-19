package jcli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// apiJob is a node in the Jenkins job tree (a folder has child jobs; a leaf has none).
type apiJob struct {
	Name string   `json:"name"`
	URL  string   `json:"url"`
	Jobs []apiJob `json:"jobs"`
}

// WalkJobs fetches the job tree (one deep query, up to 5 folder levels) and
// returns the buildable LEAF jobs with their full /-separated paths. Folders
// nested deeper than 5 levels are not reached in one query — bump the tree string
// if a real instance needs it.
func (c *Client) WalkJobs(ctx context.Context) ([]CachedJob, error) {
	const tree = "jobs[name,url,jobs[name,url,jobs[name,url,jobs[name,url,jobs[name,url]]]]]"
	resp, err := c.do(ctx, http.MethodGet, "/api/json?tree="+tree, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errs.Remote("JOBS_FETCH_FAILED", "jobs fetch HTTP "+resp.Status)
	}
	var root struct {
		Jobs []apiJob `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil, errs.Remote("JOBS_DECODE", err.Error())
	}
	var out []CachedJob
	for _, j := range root.Jobs {
		collectJobs(j, nil, &out)
	}
	return out, nil
}

// collectJobs walks a node: a node with no child jobs is a buildable leaf.
func collectJobs(node apiJob, prefix []string, out *[]CachedJob) {
	path := append(append([]string{}, prefix...), node.Name)
	if len(node.Jobs) == 0 {
		*out = append(*out, CachedJob{Name: node.Name, Path: strings.Join(path, "/"), URL: node.URL})
		return
	}
	for _, child := range node.Jobs {
		collectJobs(child, path, out)
	}
}
