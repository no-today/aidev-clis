package jcli

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Whoami returns the authenticated Jenkins user id, proving the instance is
// reachable AND the credential authenticates (used by `jcli doctor`).
func (c *Client) Whoami(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/me/api/json?tree=id,fullName", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errs.Remote("WHOAMI_FAILED", "/me/api/json returned HTTP "+resp.Status)
	}
	var me struct {
		ID       string `json:"id"`
		FullName string `json:"fullName"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	if me.ID != "" {
		return me.ID, nil
	}
	return me.FullName, nil
}
