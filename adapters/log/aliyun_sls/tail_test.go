package aliyunsls

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTail_EmitsAndStopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-log-progress", "Complete")
		w.Header().Set("x-log-count", "2")
		_, _ = w.Write([]byte(`[{"msg":"line1"},{"msg":"line2"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())

	var collected []map[string]interface{}
	err := c.Tail(ctx, "ls1", "*", 10*time.Millisecond, func(rec map[string]interface{}) error {
		collected = append(collected, rec)
		if len(collected) >= 2 {
			cancel()
		}
		return nil
	})

	if err == nil {
		t.Fatal("want non-nil error after cancel")
	}
	if !strings.Contains(err.Error(), "SLS_TAIL_CTX_CANCELLED") {
		t.Fatalf("want SLS_TAIL_CTX_CANCELLED, got %v", err)
	}
	if len(collected) < 2 {
		t.Fatalf("want at least 2 records delivered, got %d", len(collected))
	}
}

// TestTail_PaginatesWindowBeyond100 is the regression test for silent log loss:
// a poll window with >MaxPerRequest matching lines must be fully paged, not
// truncated at the first page when the window advances.
func TestTail_PaginatesWindowBeyond100(t *testing.T) {
	page := func(n int) []map[string]interface{} {
		logs := make([]map[string]interface{}, n)
		for i := range logs {
			logs[i] = map[string]interface{}{"msg": "line"}
		}
		return logs
	}
	var sawSecondPage atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("offset") {
		case "0":
			writeLogs(w, page(MaxPerRequest), "Complete") // full page → must keep paging
		case strconv.Itoa(MaxPerRequest):
			sawSecondPage.Store(true)
			writeLogs(w, page(50), "Complete") // short page → window done
		default:
			writeLogs(w, nil, "Complete")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	collected := 0
	err := c.Tail(ctx, "ls1", "*", time.Millisecond, func(rec map[string]interface{}) error {
		collected++
		if collected >= MaxPerRequest+50 {
			cancel()
		}
		return nil
	})
	if !strings.Contains(err.Error(), "SLS_TAIL_CTX_CANCELLED") {
		t.Fatalf("want cancel error, got %v", err)
	}
	// Distinguishing assertion: the fix pages to offset=MaxPerRequest WITHIN one
	// window. The pre-fix code only ever queried offset=0 (re-polling each window),
	// so this stays false for the buggy implementation — even though 150 rows would
	// still eventually trickle in across polls and a count-only check would pass.
	if !sawSecondPage.Load() {
		t.Fatal("offset=MaxPerRequest never fetched — window not paged (H1 regression)")
	}
	if collected < MaxPerRequest+50 {
		t.Fatalf("want all %d windowed rows emitted, got %d (logs dropped past page 1)", MaxPerRequest+50, collected)
	}
}

func TestTail_PropagatesEmitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-log-progress", "Complete")
		w.Header().Set("x-log-count", "2")
		_, _ = w.Write([]byte(`[{"msg":"line1"},{"msg":"line2"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	sentinel := errors.New("downstream closed")
	calls := 0
	err := c.Tail(context.Background(), "ls1", "*", 10*time.Millisecond, func(rec map[string]interface{}) error {
		calls++
		return sentinel // fail on the very first emit
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel emit error propagated, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("emit should stop after the first error, got %d calls", calls)
	}
}
