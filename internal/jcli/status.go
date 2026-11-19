package jcli

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// ResolveBuild returns n if n>0, else the job's last build number.
func (c *Client) ResolveBuild(ctx context.Context, job string, n int) (int, error) {
	if n > 0 {
		return n, nil
	}
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/api/json?tree=lastBuild[number]", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, errs.Remote("JOB_NOT_FOUND", "no such job: "+job)
	}
	var j struct {
		LastBuild *struct {
			Number int `json:"number"`
		} `json:"lastBuild"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&j)
	if j.LastBuild == nil || j.LastBuild.Number == 0 {
		return 0, errs.Remote("NO_BUILDS", "job "+job+" has no builds yet")
	}
	return j.LastBuild.Number, nil
}

// Status fetches a single build's current state (does not wait).
func (c *Client) Status(ctx context.Context, job string, build int) (BuildResult, error) {
	n, err := c.ResolveBuild(ctx, job, build)
	if err != nil {
		return BuildResult{}, err
	}
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/"+itoa(n)+"/api/json", nil)
	if err != nil {
		return BuildResult{}, err
	}
	defer resp.Body.Close()
	var b struct {
		Building bool   `json:"building"`
		Result   string `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&b)
	return BuildResult{Job: job, Build: n, Result: b.Result, Building: b.Building, URL: c.base + jobPath(job) + "/" + itoa(n) + "/"}, nil
}
