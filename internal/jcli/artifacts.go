package jcli

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Artifact is one archived build artifact.
type Artifact struct {
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
}

// Artifacts returns a build's archived artifacts. An empty list (the build
// archived nothing — e.g. an image-push pipeline) is a valid result, not an error.
func (c *Client) Artifacts(ctx context.Context, job string, build int) ([]Artifact, error) {
	const tree = "artifacts[fileName,relativePath]"
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/"+itoa(build)+"/api/json?tree="+tree, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errs.Remote("JOB_NOT_FOUND", "no such build: "+job+" #"+itoa(build))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errs.Remote("ARTIFACTS_FAILED", "artifacts fetch HTTP "+resp.Status)
	}
	var j struct {
		Artifacts []Artifact `json:"artifacts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return nil, errs.Remote("ARTIFACTS_DECODE", err.Error())
	}
	return j.Artifacts, nil
}
