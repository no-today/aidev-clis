package sqlcore

import (
	"context"
	"database/sql"
)

// watchCancel starts a goroutine that, if ctx is cancelled before stop() is
// called, asks the dialect to cancel the server-side query identified by
// backendID (via a fresh connection from db). stop() ends the watch; calling it
// after normal completion guarantees no cancel fires. Returns the stop func.
func watchCancel(ctx context.Context, d Dialect, db *sql.DB, backendID string) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Use a background context for the kill — ctx is already dead.
			_ = d.CancelQuery(context.Background(), db, backendID)
		case <-done:
		}
	}()
	return func() { close(done) }
}
