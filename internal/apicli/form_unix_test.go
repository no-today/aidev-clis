//go:build unix

package apicli

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// TestEncodeFormRejectsFIFO is the case that motivated the regular-file check:
// os.Stat reports Size() == 0 for a FIFO, so a naive "reject directories only"
// guard lets it through the --max-upload cap check, and the subsequent
// io.Copy then blocks forever waiting for a writer (or, for /dev/zero, reads
// an unbounded stream). This file is unix-only (no syscall.Mkfifo on
// Windows), matching the //go:build unix convention used elsewhere in this
// repo (see internal/core/cred/perm_unix_test.go) — the whole file is
// excluded from the Windows build/test run rather than skipped at runtime.
func TestEncodeFormRejectsFIFO(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	parts, err := ParseFormArgs([]string{"f=@" + fifo})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	done := make(chan struct{})
	var body []byte
	var ct string
	var encodeErr error
	go func() {
		body, ct, encodeErr = EncodeForm(parts, 1<<20)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("EncodeForm blocked on a FIFO instead of rejecting it promptly")
	}

	if encodeErr == nil {
		t.Fatalf("expected FORM_FILE_UNREADABLE for a FIFO, got body=%q ct=%q", body, ct)
	}
	if code := errs.From(encodeErr).Code; code != "FORM_FILE_UNREADABLE" {
		t.Errorf("FIFO: code = %q, want FORM_FILE_UNREADABLE", code)
	}
}
