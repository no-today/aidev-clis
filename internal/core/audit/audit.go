// Package audit appends one JSON line per CLI invocation to a dated day-file
// under $AIDEV_CLIS_HOME/audit/ (or ~/.aidev-clis/audit/): one file per UTC day,
// named <YYYYMMDD>.jsonl. Writes are best-effort (never block or fail the
// operation). Day-files older than 30 days are pruned on write. Payloads are NOT
// redacted — the log is 0600 and an agent can already reach the backend; see the
// design spec.
package audit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

const retentionDays = 30

// nowFn is the clock, overridable in tests.
var nowFn = func() time.Time { return time.Now().UTC() }

// Record is the caller's description of one operation. Descriptive only — the
// audit package assigns id/time/outcome and owns persistence.
type Record struct {
	Tool          string
	Backend       string
	Target        string
	Command       string         // full argv, via CommandLine
	Request       map[string]any // resolved backend params (apicli, jcli)
	AllowWrite    bool           // dbcli only
	SideEffecting bool           // if true: Begin assigns an id + writes a "started" line
}

// line is the persisted JSON shape.
type line struct {
	Time       string         `json:"time"`
	ID         string         `json:"id,omitempty"`
	Session    string         `json:"session,omitempty"`
	Tool       string         `json:"tool"`
	Backend    string         `json:"backend,omitempty"`
	Target     string         `json:"target,omitempty"`
	Command    string         `json:"command"`
	Request    map[string]any `json:"request,omitempty"`
	AllowWrite bool           `json:"allow_write,omitempty"`
	Outcome    string         `json:"outcome"`
	Code       string         `json:"code,omitempty"`
	DurationMS *int64         `json:"duration_ms,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
}

// sessionID correlates lines from one agent session. Claude Code injects
// CLAUDE_CODE_SESSION_ID into every command it runs; AIDEV_SESSION is the
// agent-agnostic override (it wins when both are set). Empty means unknown
// and the field is omitted.
func sessionID() string {
	if s := os.Getenv("AIDEV_SESSION"); s != "" {
		return s
	}
	return os.Getenv("CLAUDE_CODE_SESSION_ID")
}

// CommandLine renders argv as a human-readable command: argv[0] reduced to its
// basename, and any token containing whitespace quoted.
func CommandLine(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	parts := make([]string, len(argv))
	parts[0] = filepath.Base(argv[0])
	copy(parts[1:], argv[1:])
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\n") {
			parts[i] = strconv.Quote(p)
		}
	}
	return strings.Join(parts, " ")
}

// newID is used by Begin/Finish (later task).
func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func warn(err error) { fmt.Fprintf(os.Stderr, "audit: %v\n", err) }

// writeLine persists ln to the day-file for t, best-effort. ln.Time must already
// be set by the caller. Never returns an error; failures go to stderr.
func writeLine(ln line, t time.Time) {
	home, err := config.Home()
	if err != nil {
		warn(err)
		return
	}
	dir := filepath.Join(home, "audit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		warn(err)
		return
	}
	path := filepath.Join(dir, t.UTC().Format("20060102")+".jsonl")
	appendLocked(path, ln)
	// prune runs AFTER the day-file lock is released (appendLocked's defers have
	// fired) so a ReadDir + removals never extend the contention window that
	// concurrent writers serialize on.
	prune(dir, t.UTC())
}

// appendLocked opens the day-file, takes the exclusive lock, appends one line,
// then releases (via defers) before returning. Best-effort; failures -> stderr.
func appendLocked(path string, ln line) {
	// Open read+write, NOT append-only: on Windows LockFileEx requires a handle
	// with GENERIC_READ/GENERIC_WRITE, which an O_APPEND handle (FILE_APPEND_DATA
	// only) lacks — locking it fails with ERROR_ACCESS_DENIED. The exclusive lock
	// below (not O_APPEND) serializes concurrent appends; we seek to EOF under the
	// lock before writing.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		warn(err)
		return
	}
	defer f.Close()
	unlock, err := lockFile(f)
	if err != nil {
		warn(err)
		return
	}
	defer unlock()
	b, err := json.Marshal(ln)
	if err != nil {
		warn(err)
		return
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		warn(err)
		return
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		warn(err)
	}
}

// prune deletes day-files whose date is more than retentionDays before ref's
// calendar day. Non-<YYYYMMDD>.jsonl files are never touched. Best-effort.
func prune(dir string, ref time.Time) {
	refDay := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := refDay.AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		datePart, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok {
			continue
		}
		d, err := time.Parse("20060102", datePart)
		if err != nil {
			continue
		}
		if d.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Op is an in-flight audited operation. Begin starts it; Finish ends it.
type Op struct {
	base  line
	start time.Time
}

// Begin stamps id (for side-effecting ops), records the base fields, and — when
// r.SideEffecting — writes the "started" line immediately, before the caller
// dispatches to the backend. A hard kill after this point still leaves the
// started line ("issued, outcome unknown").
func Begin(r Record) *Op {
	op := &Op{
		base: line{
			Session: sessionID(),
			Tool:    r.Tool, Backend: r.Backend, Target: r.Target,
			Command: r.Command, Request: r.Request, AllowWrite: r.AllowWrite,
		},
		start: nowFn(),
	}
	if r.SideEffecting {
		op.base.ID = newID()
		started := op.base // shallow copy; Request map is shared but never mutated
		started.Time = op.start.Format(time.RFC3339Nano)
		started.Outcome = "started"
		writeLine(started, op.start)
	}
	return op
}

// Finish writes the terminal line: ok when err == nil, else error + errs code.
// result is attached as-is (may be nil). The terminal line carries duration_ms
// measured from Begin (the started line, if any, carries none).
func (op *Op) Finish(err error, result map[string]any) {
	t := nowFn()
	ln := op.base
	ln.Time = t.Format(time.RFC3339Nano)
	ln.Result = result
	ms := t.Sub(op.start).Milliseconds()
	ln.DurationMS = &ms
	if err != nil {
		ln.Outcome = "error"
		ln.Code = errs.From(err).Code
	} else {
		ln.Outcome = "ok"
	}
	writeLine(ln, t)
}
