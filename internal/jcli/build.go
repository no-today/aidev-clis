package jcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// BuildResult is the outcome of triggering / waiting on a build.
type BuildResult struct {
	Job       string     `json:"job"`
	Build     int        `json:"build"`
	Result    string     `json:"result,omitempty"` // SUCCESS/FAILURE/ABORTED/UNSTABLE/"" while building
	Building  bool       `json:"building,omitempty"`
	URL       string     `json:"url"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Trigger POSTs buildWithParameters and resolves the queue item to a build number.
func (c *Client) Trigger(ctx context.Context, job string, params map[string]string) (BuildResult, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	resp, err := c.do(ctx, http.MethodPost, jobPath(job)+"/buildWithParameters", form)
	if err != nil {
		return BuildResult{}, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return BuildResult{}, errs.Remote("JOB_NOT_FOUND", "no such job: "+job)
	}
	if resp.StatusCode != http.StatusCreated {
		return BuildResult{}, errs.Remote("BUILD_FAILED", "trigger returned HTTP "+resp.Status)
	}
	queueURL := resp.Header.Get("Location")
	if queueURL == "" {
		return BuildResult{}, errs.Remote("BUILD_FAILED", "Jenkins returned no queue Location")
	}
	num, err := c.awaitQueue(ctx, queueURL)
	if err != nil {
		return BuildResult{}, err
	}
	return BuildResult{Job: job, Build: num, URL: c.base + jobPath(job) + "/" + itoa(num) + "/"}, nil
}

func (c *Client) awaitQueue(ctx context.Context, queueURL string) (int, error) {
	for {
		resp, err := c.do(ctx, http.MethodGet, queueURL+"api/json", nil)
		if err != nil {
			return 0, err
		}
		var q struct {
			Executable *struct {
				Number int `json:"number"`
			} `json:"executable"`
			Cancelled bool `json:"cancelled"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&q)
		resp.Body.Close()
		if q.Cancelled {
			return 0, errs.Remote("BUILD_FAILED", "queued build was cancelled before it started")
		}
		if q.Executable != nil && q.Executable.Number > 0 {
			return q.Executable.Number, nil
		}
		if err := sleepCtx(ctx, c.pollEvery); err != nil {
			return 0, errs.Timeout("BUILD_TIMEOUT", err.Error())
		}
	}
}

// Wait polls a build until it finishes and returns the final result. A non-SUCCESS
// result is returned both in BuildResult and as a BUILD_FAILED error.
func (c *Client) Wait(ctx context.Context, job string, build int) (BuildResult, error) {
	for {
		resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/"+itoa(build)+"/api/json", nil)
		if err != nil {
			// A deadline that fires mid-request surfaces as a transport error; it's
			// a timeout (exit 4), not a remote failure (exit 5).
			if ctx.Err() != nil {
				return BuildResult{}, errs.Timeout("BUILD_TIMEOUT", ctx.Err().Error())
			}
			return BuildResult{}, err
		}
		var b struct {
			Building bool   `json:"building"`
			Result   string `json:"result"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&b)
		resp.Body.Close()
		if !b.Building {
			res := BuildResult{Job: job, Build: build, Result: b.Result, URL: c.base + jobPath(job) + "/" + itoa(build) + "/"}
			if b.Result != "SUCCESS" {
				return res, errs.Remote("BUILD_FAILED", "build "+itoa(build)+" finished "+b.Result)
			}
			return res, nil
		}
		if err := sleepCtx(ctx, c.pollEvery); err != nil {
			return BuildResult{}, errs.Timeout("BUILD_TIMEOUT", err.Error())
		}
	}
}

// sleepCtx waits d (or returns immediately if d<=0), honoring ctx cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
