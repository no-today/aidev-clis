package aliyunsls

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestTrace_BuildsFieldQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("x-log-progress", "Complete")
		w.Header().Set("x-log-count", "0")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if _, err := c.Trace(context.Background(), "ls1", "traceId", "abc123", 100, 200, 500); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotQuery != "traceId: abc123" {
		t.Fatalf("query = %q, want 'traceId: abc123'", gotQuery)
	}
}

func TestTrace_FallsBackToFullTextWhenTraceFieldIsNotIndexed(t *testing.T) {
	var gotQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		gotQueries = append(gotQueries, query)
		if query == "traceId: abc123" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorCode":"ParameterInvalid","errorMessage":"traceId field is not queryable"}`))
			return
		}
		writeLogs(w, []map[string]interface{}{{"traceId": "abc123", "message": "found by full text"}}, "Complete")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.Trace(context.Background(), "ls1", "traceId", "abc123", 100, 200, 500)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	wantQueries := []string{"traceId: abc123", "abc123"}
	if !reflect.DeepEqual(gotQueries, wantQueries) {
		t.Fatalf("queries = %v, want %v", gotQueries, wantQueries)
	}
	if len(res.Logs) != 1 || res.Logs[0]["message"] != "found by full text" {
		t.Fatalf("fallback logs = %+v", res.Logs)
	}
}

func TestTrace_RecordsDiagnosticWhenFallingBackToFullText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "traceId: abc123" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorCode":"ParameterInvalid","errorMessage":"traceId field is not queryable"}`))
			return
		}
		writeLogs(w, nil, "Complete")
	}))
	defer srv.Close()

	d := diag.New(1)
	ctx := diag.With(context.Background(), d)
	c := newTestClient(t, srv.URL)
	if _, err := c.Trace(ctx, "ls1", "traceId", "abc123", 100, 200, 500); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	lines := d.Lines()
	if len(lines) != 1 {
		t.Fatalf("want one diagnostic line, got %v", lines)
	}
	if !strings.Contains(lines[0], "trace field query failed") ||
		!strings.Contains(lines[0], "field=traceId") ||
		!strings.Contains(lines[0], "remote_status=400") ||
		!strings.Contains(lines[0], "remote_code=ParameterInvalid") ||
		!strings.Contains(lines[0], "falling back to full-text search") {
		t.Fatalf("diagnostic line missing fallback details: %q", lines[0])
	}
	if strings.Contains(lines[0], "abc123") {
		t.Fatalf("diagnostic line must not include trace id: %q", lines[0])
	}
}
