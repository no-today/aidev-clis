package jcli

import (
	"context"
	"net/http"
	"strconv"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Cancel aborts a running build (POST .../stop). A non-2xx is reported.
func (c *Client) Cancel(ctx context.Context, job string, build int) error {
	resp, err := c.do(ctx, http.MethodPost, jobPath(job)+"/"+strconv.Itoa(build)+"/stop", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusFound {
		return errs.Remote("CANCEL_FAILED", "stop returned HTTP "+resp.Status)
	}
	return nil
}
