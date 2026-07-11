package aliyunsls

import (
	"context"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Tail polls GetLogs with a sliding window and calls emit for each new log line
// in chronological order. Returns when ctx is cancelled. pollInterval defaults
// to 5s when non-positive.
//
// Known limitation: after each poll `lastTo` advances to `now`, so a log whose
// server-side ingest lags past that boundary (indexed with a receive time before
// `now` but not yet queryable when we polled) can fall outside every window and
// be missed. This is inherent to time-windowed GetLogs polling, not a bug —
// there is no exactly-once cursor. The initial 30s look-back (lastTo = now-30)
// tolerates typical ingest lag; heavier lag can still drop lines. Use bounded
// historical queries, not tail, when completeness matters.
func (c *Client) Tail(ctx context.Context, logstore, query string, pollInterval time.Duration, emit func(map[string]interface{}) error) error {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	lastTo := time.Now().Unix() - 30
	for {
		select {
		case <-ctx.Done():
			return errs.Timeout("SLS_TAIL_CTX_CANCELLED", ctx.Err().Error())
		default:
		}
		now := time.Now().Unix()
		// Page through the whole window. A single GetLogs call returns at most
		// MaxPerRequest rows; without this loop any window with >100 matching
		// lines would silently drop everything past the first page when lastTo
		// advances. reverse=false keeps chronological (left-to-right) emission.
		for offset := 0; ; {
			result, err := c.singleCall(ctx, logstore, query, lastTo, now, MaxPerRequest, offset, false)
			if err != nil {
				return err
			}
			for _, log := range result.Logs {
				select {
				case <-ctx.Done():
					return errs.Timeout("SLS_TAIL_CTX_CANCELLED", ctx.Err().Error())
				default:
				}
				if err := emit(log); err != nil {
					return err
				}
			}
			if len(result.Logs) < MaxPerRequest {
				break // short page → window exhausted
			}
			offset += len(result.Logs)
		}
		lastTo = now
		select {
		case <-ctx.Done():
			return errs.Timeout("SLS_TAIL_CTX_CANCELLED", ctx.Err().Error())
		case <-time.After(pollInterval):
		}
	}
}
