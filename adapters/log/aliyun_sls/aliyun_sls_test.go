package aliyunsls

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/logcli"
)

// captureOut is a minimal logcli.Output that records the last Batch payload so
// verb tests can assert on what was emitted.
type captureOut struct{ batch []any }

func (o *captureOut) Batch(records []any, _ ...string) error { o.batch = records; return nil }
func (o *captureOut) Stream() logcli.Streamer                { return nil }

// doctor reports staged checks (config → connect) matching jcli/dbcli; the
// connect stage probes the configured logstore via the real GetLogs path.
func TestDoctor_OKStagedChecks(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("x-log-progress", "Complete")
		w.Header().Set("x-log-count", "0")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	cfg := &SLSConfig{Project: "p", Logstore: "app-service-log", Endpoint: "e", TraceField: "traceId", Credential: "sls.ak"}
	out := &captureOut{}
	if err := doDoctor(context.Background(), c, cfg, out); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Must hit the configured logstore's GetLogs path, not a project-level probe.
	if !strings.Contains(gotPath, "app-service-log") {
		t.Fatalf("doctor probed %q, want the configured logstore's GetLogs path", gotPath)
	}
	if len(out.batch) != 2 {
		t.Fatalf("doctor emitted %d checks, want 2 (config, connect)", len(out.batch))
	}
	cfgChk, ok := out.batch[0].(check)
	if !ok || cfgChk.Name != "config" || !cfgChk.OK {
		t.Fatalf("check[0] = %+v, want config ok", out.batch[0])
	}
	conn, ok := out.batch[1].(check)
	if !ok || conn.Name != "connect" || !conn.OK {
		t.Fatalf("check[1] = %+v, want connect ok", out.batch[1])
	}
}

// On a failing probe (auth/permission/wrong logstore) doctor still emits the
// checks (connect ok:false) and returns the error to carry a non-zero exit.
func TestDoctor_ProbeFailureIsConnectCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"Unauthorized","errorMessage":"no permission"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	cfg := &SLSConfig{Project: "p", Logstore: "l", Endpoint: "e", TraceField: "traceId", Credential: "sls.ak"}
	out := &captureOut{}
	err := doDoctor(context.Background(), c, cfg, out)
	if err == nil || !strings.Contains(err.Error(), "SLS_AUTH_FAILED") {
		t.Fatalf("want SLS_AUTH_FAILED, got %v", err)
	}
	if len(out.batch) != 2 {
		t.Fatalf("doctor emitted %d checks, want 2 even on failure", len(out.batch))
	}
	conn, ok := out.batch[1].(check)
	if !ok || conn.Name != "connect" || conn.OK {
		t.Fatalf("check[1] = %+v, want connect ok:false", out.batch[1])
	}
}

func TestParseArgs(t *testing.T) {
	// Positional subject first, then modifier flags in any order.
	pos, got := parseArgs([]string{"level: ERROR", "--reverse", "--size", "50", "--to=now"})
	if pos != "level: ERROR" {
		t.Fatalf("positional = %q, want 'level: ERROR'", pos)
	}
	want := map[string]string{"reverse": "true", "size": "50", "to": "now"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseArgs flags = %v, want %v", got, want)
	}

	// A flag value preceding the positional must not be mistaken for it.
	pos, _ = parseArgs([]string{"--from", "1h", "abc123"})
	if pos != "abc123" {
		t.Fatalf("positional = %q, want 'abc123' (flag value must not be the positional)", pos)
	}

	// No positional → empty; callers default it (query→"*", trace→required error).
	if pos, _ := parseArgs([]string{"--size", "10"}); pos != "" {
		t.Fatalf("positional = %q, want empty", pos)
	}
}

func TestSplitVerbAndArgs_AllowsRangeFlagsBeforeVerb(t *testing.T) {
	verb, rest := splitVerbAndArgs([]string{
		"--from", "1700000000",
		"--to", "1700003600",
		"search",
		"level: ERROR",
		"--size", "50",
	})
	if verb != "search" {
		t.Fatalf("verb = %q, want search", verb)
	}
	pos, flags := parseArgs(rest)
	if pos != "level: ERROR" {
		t.Fatalf("positional = %q, want level: ERROR", pos)
	}
	want := map[string]string{"from": "1700000000", "to": "1700003600", "size": "50"}
	if !reflect.DeepEqual(flags, want) {
		t.Fatalf("flags = %v, want %v", flags, want)
	}

	verb, rest = splitVerbAndArgs([]string{"--reverse", "search", "level: ERROR"})
	if verb != "search" {
		t.Fatalf("verb = %q, want search", verb)
	}
	pos, flags = parseArgs(rest)
	if pos != "level: ERROR" || flags["reverse"] != "true" {
		t.Fatalf("rest parsed as pos=%q flags=%v, want positional with reverse=true", pos, flags)
	}
}

func TestDoSearch_SendsExplicitTimeRange(t *testing.T) {
	var gotFrom, gotTo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFrom = r.URL.Query().Get("from")
		gotTo = r.URL.Query().Get("to")
		w.Header().Set("x-log-progress", "Complete")
		w.Header().Set("x-log-count", "0")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	cfg := &SLSConfig{Project: "p", Logstore: "l", Endpoint: "e", TraceField: "traceId", Credential: "sls.ak"}
	out := &captureOut{}
	from := int64(1700000000)
	to := int64(1700003600)
	if err := doSearch(context.Background(), c, cfg, "*", map[string]string{
		"from": strconv.FormatInt(from, 10),
		"to":   strconv.FormatInt(to, 10),
	}, out); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotFrom != strconv.FormatInt(from, 10) || gotTo != strconv.FormatInt(to, 10) {
		t.Fatalf("SLS query range from/to = %s/%s, want %d/%d", gotFrom, gotTo, from, to)
	}
}

func TestFlagOrAndIntOr(t *testing.T) {
	f := map[string]string{"size": "7", "query": ""}
	if flagOr(f, "query", "*") != "*" {
		t.Fatal("empty value should fall back to default")
	}
	if flagOr(f, "from", "1h") != "1h" {
		t.Fatal("missing key should use default")
	}
	if intOr(f, "size", 100) != 7 {
		t.Fatalf("intOr size")
	}
	if intOr(f, "missing", 100) != 100 {
		t.Fatalf("intOr default")
	}
	// non-positive size must fall back to the default, not loop zero times.
	if intOr(map[string]string{"size": "0"}, "size", 100) != 100 {
		t.Fatalf("intOr should reject 0")
	}
	if intOr(map[string]string{"size": "-5"}, "size", 100) != 100 {
		t.Fatalf("intOr should reject negative")
	}
}

func TestName(t *testing.T) {
	if New().Name() != "sls" {
		t.Fatal("name")
	}
}

func TestLogsToAnyNil(t *testing.T) {
	if len(logsToAny(nil)) != 0 {
		t.Fatal("nil result → empty slice")
	}
}
