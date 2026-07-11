package aliyunsls

import (
	"context"
	"fmt"
	"net/http"

	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Trace fetches all logs whose traceField equals traceID within the window.
// If traceField isn't configured as a key-value index, SLS rejects the field
// query with ParameterInvalid; we catch that (isTraceFieldIndexError) and fall
// back to a full-text search on the bare traceID.
func (c *Client) Trace(ctx context.Context, logstore, traceField, traceID string, fromTS, toTS int64, size int) (*LogResult, error) {
	query := fmt.Sprintf("%s: %s", traceField, traceID)
	res, err := c.Search(ctx, logstore, query, fromTS, toTS, size, false)
	if err == nil {
		return res, nil
	}
	if isTraceFieldIndexError(err) {
		e := errs.From(err)
		diag.From(ctx).Logf(1,
			"trace field query failed field=%s remote_status=%d remote_code=%s; falling back to full-text search",
			traceField, e.RemoteStatus, e.RemoteCode)
		return c.Search(ctx, logstore, traceID, fromTS, toTS, size, false)
	}
	return nil, err
}

func isTraceFieldIndexError(err error) bool {
	e, ok := err.(*errs.Error)
	return ok &&
		e.Code == "SLS_API_ERROR" &&
		e.RemoteStatus == http.StatusBadRequest &&
		e.RemoteCode == "ParameterInvalid"
}
