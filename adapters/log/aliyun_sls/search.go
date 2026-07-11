package aliyunsls

import (
	"context"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Search fetches up to `size` rows, paginating via offset when size exceeds
// MaxPerRequest. fromTS/toTS are unix seconds. reverse=true returns newest
// first.
func (c *Client) Search(ctx context.Context, logstore, query string, fromTS, toTS int64, size int, reverse bool) (*LogResult, error) {
	merged := &LogResult{}
	remaining := size
	offset := 0
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return nil, errs.Timeout("SLS_CTX_CANCELLED", ctx.Err().Error())
		default:
		}
		line := remaining
		if line > MaxPerRequest {
			line = MaxPerRequest
		}
		r, err := c.singleCall(ctx, logstore, query, fromTS, toTS, line, offset, reverse)
		if err != nil {
			return nil, err
		}
		merged.Logs = append(merged.Logs, r.Logs...)
		merged.Progress = r.Progress
		got := len(r.Logs)
		if got < line {
			break // short page → exhausted
		}
		offset += got
		remaining -= got
	}
	merged.Count = len(merged.Logs)
	merged.Total = merged.Count
	return merged, nil
}
