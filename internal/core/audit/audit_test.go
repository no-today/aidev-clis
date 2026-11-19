package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func TestCommandLine_BasenameAndQuoting(t *testing.T) {
	got := CommandLine([]string{"/usr/local/bin/dbcli", "mysql", "select * from t", "--target", "zssy"})
	want := `dbcli mysql "select * from t" --target zssy`
	if got != want {
		t.Fatalf("CommandLine = %q, want %q", got, want)
	}
	if CommandLine(nil) != "" {
		t.Fatal("CommandLine(nil) should be empty")
	}
}

// writeAt is an in-package test helper that writes a line at a specific timestamp.
func writeAt(t *testing.T, ts string, ln line) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatal(err)
	}
	ln.Time = parsed.Format(time.RFC3339Nano)
	writeLine(ln, parsed)
}

func TestRecord_AllowWriteOmitempty(t *testing.T) {
	b, _ := json.Marshal(line{Tool: "dbcli", AllowWrite: true})
	if !strings.Contains(string(b), `"allow_write":true`) {
		t.Fatalf("allow_write should serialize when true: %s", b)
	}
	b, _ = json.Marshal(line{Tool: "dbcli"})
	if strings.Contains(string(b), "allow_write") {
		t.Fatalf("allow_write should be omitted when false: %s", b)
	}
}

// writeLine writes to audit/<YYYYMMDD>.jsonl, with the date taken from the timestamp.
func TestAppend_WritesDatedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	writeAt(t, "2026-07-01T01:29:40Z", line{Tool: "logcli", Backend: "local-file", Target: "app", Command: "logs", Outcome: "ok"})
	b, err := os.ReadFile(filepath.Join(dir, "audit", "20260701.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(string(b)), "\n") != 0 {
		t.Fatalf("want exactly one line: %q", b)
	}
	var ln line
	if err := json.Unmarshal(b, &ln); err != nil {
		t.Fatal(err)
	}
	if ln.Tool != "logcli" || ln.Outcome != "ok" {
		t.Fatalf("bad record: %+v", ln)
	}
}

// Begin/Finish stamps a timestamp; the line lands in exactly one day-file.
func TestAppend_StampsTimestamp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)

	fixed := time.Date(2026, 7, 1, 1, 29, 40, 123456789, time.UTC)
	orig := nowFn
	nowFn = func() time.Time { return fixed }
	defer func() { nowFn = orig }()

	Begin(Record{Tool: "dbcli", Command: "x"}).Finish(nil, nil)
	files, _ := filepath.Glob(filepath.Join(dir, "audit", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("want exactly one day-file, got %v", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var ln line
	if err := json.Unmarshal(b, &ln); err != nil {
		t.Fatal(err)
	}
	if ln.Time == "" {
		t.Fatal("Begin/Finish must stamp a timestamp (invariant: every line is timestamped)")
	}
	parsed, err := time.Parse(time.RFC3339Nano, ln.Time)
	if err != nil {
		t.Fatalf("time %q must parse as RFC3339Nano: %v", ln.Time, err)
	}
	if !parsed.Equal(fixed) {
		t.Fatalf("time = %v, want %v", parsed, fixed)
	}
	if ln.Time != fixed.Format(time.RFC3339Nano) {
		t.Fatalf("time = %q, want %q", ln.Time, fixed.Format(time.RFC3339Nano))
	}
}

// Day-files more than 30 days older than the record's date are pruned; the
// boundary day and anything that is not a YYYYMMDD.jsonl file are untouched.
func TestAppend_PrunesOlderThan30Days(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	auditDir := filepath.Join(dir, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// name -> should survive an Append dated 2026-07-01
	seed := map[string]bool{
		"20260501.jsonl": false, // 61d old -> pruned
		"20260531.jsonl": false, // 31d old -> pruned (just over the boundary)
		"20260601.jsonl": true,  // exactly 30d -> kept (boundary)
		"20260620.jsonl": true,  // 11d old -> kept
		"notes.txt":      true,  // not a day-file -> never touched
		"latest.jsonl":   true,  // .jsonl but not a date -> never touched
	}
	for name := range seed {
		if err := os.WriteFile(filepath.Join(auditDir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeAt(t, "2026-07-01T01:29:40Z", line{Tool: "jcli", Outcome: "ok"})
	for name, shouldSurvive := range seed {
		_, err := os.Stat(filepath.Join(auditDir, name))
		exists := err == nil
		if exists != shouldSurvive {
			t.Errorf("%s: exists=%v, want survive=%v", name, exists, shouldSurvive)
		}
	}
	if _, err := os.Stat(filepath.Join(auditDir, "20260701.jsonl")); err != nil {
		t.Fatalf("today's day-file must be written: %v", err)
	}
}

func TestWriteLine_ConcurrentLargePayloadsNoTear(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	const n = 40
	big := strings.Repeat("x", 70*1024) // >64KiB, past single-write atomicity guarantees
	ts, _ := time.Parse(time.RFC3339Nano, "2026-07-01T00:00:00Z")
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			writeLine(line{
				Time: ts.Format(time.RFC3339Nano), Tool: "dbcli",
				Command: big, Outcome: "ok", Result: map[string]any{"i": i},
			}, ts)
		}(i)
	}
	wg.Wait()
	b, err := os.ReadFile(filepath.Join(dir, "audit", "20260701.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d", len(lines), n)
	}
	for i, l := range lines {
		var out line
		if err := json.Unmarshal([]byte(l), &out); err != nil {
			t.Fatalf("line %d is not valid JSON (torn write): %v", i, err)
		}
	}
}

func readLines(t *testing.T, dir string) []line {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "audit", "*.jsonl"))
	var out []line
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l == "" {
				continue
			}
			var ln line
			if err := json.Unmarshal([]byte(l), &ln); err != nil {
				t.Fatal(err)
			}
			out = append(out, ln)
		}
	}
	return out
}

func TestBeginFinish_SideEffectingWritesStartedThenTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	op := Begin(Record{Tool: "dbcli", Backend: "mysql", Target: "zssy",
		Command: "dbcli mysql \"delete\" --allow-write", AllowWrite: true, SideEffecting: true})
	op.Finish(nil, map[string]any{"affected": 3})
	ls := readLines(t, dir)
	if len(ls) != 2 {
		t.Fatalf("want 2 lines, got %d", len(ls))
	}
	if ls[0].Outcome != "started" || ls[1].Outcome != "ok" {
		t.Fatalf("outcomes = %q,%q", ls[0].Outcome, ls[1].Outcome)
	}
	if ls[0].ID == "" || ls[0].ID != ls[1].ID {
		t.Fatalf("ids must be set and equal: %q,%q", ls[0].ID, ls[1].ID)
	}
	if ls[1].Result["affected"].(float64) != 3 {
		t.Fatalf("terminal result missing affected: %+v", ls[1].Result)
	}
	if ls[0].Result != nil {
		t.Fatalf("started line must not carry a result: %+v", ls[0].Result)
	}
}

func TestBeginFinish_SideEffectingErrorSharesID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	Begin(Record{Tool: "dbcli", Command: "x", SideEffecting: true}).
		Finish(errs.Config("SOME_ERR", "msg"), nil)
	ls := readLines(t, dir)
	if len(ls) != 2 {
		t.Fatalf("want 2 lines, got %d", len(ls))
	}
	if ls[1].Outcome != "error" || ls[1].Code != "SOME_ERR" {
		t.Fatalf("terminal: %+v", ls[1])
	}
	if ls[0].ID == "" || ls[0].ID != ls[1].ID {
		t.Fatalf("ids must match: %q vs %q", ls[0].ID, ls[1].ID)
	}
}

func TestBeginFinish_ReadSingleLineNoID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	Begin(Record{Tool: "logcli", Backend: "k8s", Command: "logcli k8s app logs"}).
		Finish(nil, map[string]any{"lines": 42})
	ls := readLines(t, dir)
	if len(ls) != 1 || ls[0].ID != "" || ls[0].Outcome != "ok" {
		t.Fatalf("want 1 id-less ok line, got %+v", ls)
	}
}

// Session comes from the environment: AIDEV_SESSION wins over
// CLAUDE_CODE_SESSION_ID; with neither set the field is omitted.
func TestBeginFinish_SessionFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-session")
	t.Setenv("AIDEV_SESSION", "")

	Begin(Record{Tool: "dbcli", Command: "x"}).Finish(nil, nil)
	if ls := readLines(t, dir); ls[0].Session != "cc-session" {
		t.Fatalf("session = %q, want cc-session", ls[0].Session)
	}

	t.Setenv("AIDEV_SESSION", "generic")
	Begin(Record{Tool: "dbcli", Command: "x", SideEffecting: true}).Finish(nil, nil)
	ls := readLines(t, dir)
	if ls[1].Session != "generic" || ls[2].Session != "generic" {
		t.Fatalf("AIDEV_SESSION must win on both lines: %q, %q", ls[1].Session, ls[2].Session)
	}
}

func TestBeginFinish_SessionOmittedWhenUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("AIDEV_SESSION", "")
	Begin(Record{Tool: "dbcli", Command: "x"}).Finish(nil, nil)
	files, _ := filepath.Glob(filepath.Join(dir, "audit", "*.jsonl"))
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "session") {
		t.Fatalf("session must be omitted when no env var is set: %s", b)
	}
}

// The terminal line carries duration_ms measured Begin→Finish; the started
// line carries none (its duration is unknowable by design).
func TestBeginFinish_DurationOnTerminalLineOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	base := time.Date(2026, 7, 1, 1, 29, 40, 0, time.UTC)
	calls := 0
	orig := nowFn
	nowFn = func() time.Time { calls++; return base.Add(time.Duration(calls-1) * 250 * time.Millisecond) }
	defer func() { nowFn = orig }()

	Begin(Record{Tool: "dbcli", Command: "x", SideEffecting: true}).Finish(nil, nil)
	ls := readLines(t, dir)
	if len(ls) != 2 {
		t.Fatalf("want 2 lines, got %d", len(ls))
	}
	if ls[0].DurationMS != nil {
		t.Fatalf("started line must not carry duration_ms: %d", *ls[0].DurationMS)
	}
	if ls[1].DurationMS == nil || *ls[1].DurationMS != 250 {
		t.Fatalf("terminal duration_ms = %v, want 250", ls[1].DurationMS)
	}
}

func TestFinish_ErrorSetsCode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", dir)
	Begin(Record{Tool: "dbcli", Command: "x"}).Finish(errs.Config("ADAPTER_MISMATCH", "no"), nil)
	ls := readLines(t, dir)
	if ls[0].Outcome != "error" || ls[0].Code != "ADAPTER_MISMATCH" {
		t.Fatalf("want error+code, got %+v", ls[0])
	}
}
