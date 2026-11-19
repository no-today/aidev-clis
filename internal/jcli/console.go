package jcli

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Console returns the full build console text.
func (c *Client) Console(ctx context.Context, job string, build int) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/"+strconv.Itoa(build)+"/consoleText", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errs.Remote("CONSOLE_FAILED", "console fetch HTTP "+resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errs.Remote("CONSOLE_FAILED", err.Error())
	}
	return string(b), nil
}

// ConsoleProgressive fetches the chunk of console after byte offset start,
// returning (text, nextOffset, moreData). Used by `log -f`.
func (c *Client) ConsoleProgressive(ctx context.Context, job string, build, start int) (string, int, bool, error) {
	path := jobPath(job) + "/" + strconv.Itoa(build) + "/logText/progressiveText?start=" + strconv.Itoa(start)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", start, false, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	next := start + len(b)
	more := resp.Header.Get("X-More-Data") == "true"
	return string(b), next, more, nil
}
