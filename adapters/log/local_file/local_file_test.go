package localfile

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/logcli"
)

// capOut captures Batch and (for -f) streamed records.
type capOut struct {
	mu      sync.Mutex
	batched []any
	stream  bool
	recs    []any
}

func (c *capOut) Batch(r []any, _ ...string) error { c.batched = r; return nil }
func (c *capOut) Stream() logcli.Streamer          { c.stream = true; return &capStream{c} }

type capStream struct{ c *capOut }

func (s *capStream) Record(rec any) error {
	s.c.mu.Lock()
	s.c.recs = append(s.c.recs, rec)
	s.c.mu.Unlock()
	return nil
}

func (c *capOut) streamed() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.recs...)
}

func runRaw(t *testing.T, raw map[string]any, args []string) *capOut {
	t.Helper()
	out := &capOut{}
	err := New().Run(context.Background(), logcli.Input{
		Target: config.Target{Name: "t", Adapter: "local-file", Raw: raw}, Args: args,
	}, out)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	return out
}

func msg(rec any) string { return rec.(map[string]any)["message"].(string) }

func TestLocalFile_LogsTailsLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	_ = os.WriteFile(p, []byte("line one\nline two\n"), 0o644)
	out := runRaw(t, map[string]any{"files": []any{p}}, []string{"logs"})
	if len(out.batched) != 2 || msg(out.batched[0]) != "line one" || msg(out.batched[1]) != "line two" {
		t.Fatalf("want 2 lines, got %v", out.batched)
	}
}

func TestLocalFile_LogsTailN(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	_ = os.WriteFile(p, []byte("a\nb\nc\nd\ne\n"), 0o644)
	out := runRaw(t, map[string]any{"files": []any{p}}, []string{"logs", "--tail", "2"})
	if len(out.batched) != 2 || msg(out.batched[0]) != "d" || msg(out.batched[1]) != "e" {
		t.Fatalf("--tail 2 should give last 2 lines, got %v", out.batched)
	}
}

func TestLocalFile_LogsMultiFileHeaders(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	_ = os.WriteFile(a, []byte("aa\n"), 0o644)
	_ = os.WriteFile(b, []byte("bb\n"), 0o644)
	out := runRaw(t, map[string]any{"files": []any{a, b}}, []string{"logs"})
	// header, aa, header, bb
	if len(out.batched) != 4 || msg(out.batched[0]) != "==> "+a+" <==" || msg(out.batched[1]) != "aa" {
		t.Fatalf("multi-file headers wrong: %v", out.batched)
	}
}

func TestLocalFile_Ls(t *testing.T) {
	out := runRaw(t, map[string]any{"files": []any{"/var/log/a.log", "/var/log/b.log"}}, []string{"ls"})
	if len(out.batched) != 2 || out.batched[0].(map[string]any)["name"] != "/var/log/a.log" {
		t.Fatalf("ls wrong: %v", out.batched)
	}
}

func TestLocalFile_NoFilesErrors(t *testing.T) {
	out := &capOut{}
	err := New().Run(context.Background(), logcli.Input{
		Target: config.Target{Adapter: "local-file", Raw: map[string]any{}}, Args: []string{"logs"},
	}, out)
	if err == nil {
		t.Fatal("want error when no files configured")
	}
}

func TestLocalFile_BadVerb(t *testing.T) {
	out := &capOut{}
	err := New().Run(context.Background(), logcli.Input{
		Target: config.Target{Adapter: "local-file", Raw: map[string]any{"files": []any{"/x"}}}, Args: []string{"nope"},
	}, out)
	if err == nil {
		t.Fatal("want error on unknown verb")
	}
}

func TestLocalFile_FollowStreamsAppends(t *testing.T) {
	old := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = old }()

	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	_ = os.WriteFile(p, []byte("first\n"), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	out := &capOut{}
	done := make(chan error, 1)
	go func() {
		done <- New().Run(ctx, logcli.Input{
			Target: config.Target{Adapter: "local-file", Raw: map[string]any{"files": []any{p}}}, Args: []string{"logs", "-f"},
		}, out)
	}()

	waitFor(t, out, "first")     // initial tail
	appendLine(t, p, "second\n") // appended after follow started
	waitFor(t, out, "second")
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("follow run error: %v", err)
	}
	if !out.stream {
		t.Fatal("-f must stream, not batch")
	}
}

func appendLine(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, out *capOut, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range out.streamed() {
			if msg(r) == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; got %v", want, out.streamed())
}

func TestLocalFile_Doctor(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	_ = os.WriteFile(p, []byte("x\n"), 0o644)
	// existing file → connect ok
	out := runRaw(t, map[string]any{"files": []any{p}}, []string{"doctor"})
	if len(out.batched) != 2 || !out.batched[1].(logcli.Check).OK {
		t.Fatalf("doctor ok: %+v", out.batched)
	}
	// missing file → connect false + non-nil err (exit)
	out2 := &capOut{}
	err := New().Run(context.Background(), logcli.Input{
		Target: config.Target{Adapter: "local-file", Raw: map[string]any{"files": []any{"/no/such/file"}}}, Args: []string{"doctor"},
	}, out2)
	if err == nil || len(out2.batched) != 2 || out2.batched[1].(logcli.Check).OK {
		t.Fatalf("doctor missing-file should fail: err=%v checks=%+v", err, out2.batched)
	}
}
