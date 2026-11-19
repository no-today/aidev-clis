package sqlcore

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"
)

// cancelSpy records whether CancelQuery was invoked.
type cancelSpy struct {
	stubDialect
	cancelled atomic.Bool
}

func (c *cancelSpy) CancelQuery(context.Context, *sql.DB, string) error {
	c.cancelled.Store(true)
	return nil
}

func TestWatchCancel_FiresOnTimeout(t *testing.T) {
	spy := &cancelSpy{}
	ctx, cancel := context.WithCancel(context.Background())
	stop := watchCancel(ctx, spy, nil, "backend-1")
	cancel() // simulate ctx deadline
	time.Sleep(20 * time.Millisecond)
	stop()
	if !spy.cancelled.Load() {
		t.Fatal("CancelQuery should fire when ctx is cancelled mid-query")
	}
}

func TestWatchCancel_QuietOnCompletion(t *testing.T) {
	spy := &cancelSpy{}
	ctx := context.Background()
	stop := watchCancel(ctx, spy, nil, "backend-1")
	stop() // query finished normally
	time.Sleep(20 * time.Millisecond)
	if spy.cancelled.Load() {
		t.Fatal("CancelQuery must NOT fire on normal completion")
	}
}
