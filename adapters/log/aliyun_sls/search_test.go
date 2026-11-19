package aliyunsls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestSearch_PaginatesByOffset(t *testing.T) {
	// 100 rows at offsets 0 and 100, then 50 at offset 200 (short page → stop).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		var n int
		switch offset {
		case 0, 100:
			n = 100
		default:
			n = 50
		}
		logs := make([]map[string]interface{}, n)
		for i := range logs {
			logs[i] = map[string]interface{}{"i": strconv.Itoa(offset + i)}
		}
		w.Header().Set("x-log-count", strconv.Itoa(n))
		w.Header().Set("x-log-progress", "Complete")
		_ = json.NewEncoder(w).Encode(logs)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.Search(context.Background(), "ls1", "*", 100, 200, 250, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(res.Logs) != 250 {
		t.Fatalf("want 250 rows, got %d", len(res.Logs))
	}
}

func TestSearch_SinglePageWhenSmall(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("x-log-progress", "Complete")
		w.Header().Set("x-log-count", "1")
		_, _ = w.Write([]byte(`[{"a":"1"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if _, err := c.Search(context.Background(), "ls1", "*", 100, 200, 50, true); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 1 {
		t.Fatalf("want single call, got %d", calls)
	}
}
