// Package localfile implements the logcli "local-file" adapter: reads CONFIGURED
// local log files (an allow-list, like ssh-file — the user can't point it at an
// arbitrary path). Verbs: logs (--tail N, -f follow) and ls. Tail/follow are
// Go-native (no shell-out) so the adapter stays cross-platform, incl. Windows.
package localfile

import (
	"bytes"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/logcli"
)

// pollInterval is how often `-f` re-checks each file for appended data. A var
// (not const) so tests can shrink it.
var pollInterval = 500 * time.Millisecond

type source struct{}

func New() logcli.Adapter { return source{} }

func (source) Name() string { return "local-file" }

func (source) Run(ctx context.Context, in logcli.Input, out logcli.Output) error {
	files := configFiles(in.Target.Raw)
	verb := "logs"
	if len(in.Args) > 0 && in.Args[0] != "" && in.Args[0][0] != '-' {
		verb = in.Args[0]
	}
	switch verb {
	case "doctor":
		return logcli.Doctor(out, strconv.Itoa(len(files))+" configured files", "all readable", func() error {
			if len(files) == 0 {
				return errs.Config("LOCAL_FILE_NO_FILES", "env block missing 'files' (a list of paths)")
			}
			for _, f := range files {
				if _, err := os.Stat(f); err != nil {
					return errs.Config("LOCAL_FILE_UNREADABLE", err.Error())
				}
			}
			return nil
		})
	case "ls":
		recs := make([]any, 0, len(files))
		for _, f := range files {
			recs = append(recs, map[string]any{"name": f})
		}
		return out.Batch(recs)
	case "logs":
		if len(files) == 0 {
			return errs.Config("LOCAL_FILE_NO_FILES", "env block missing 'files' (a list of paths)")
		}
		n := tailLines(in.Args)
		follow := hasFollow(in.Args)
		return logcli.StreamLines(out, in.Args, func(emit func(string) error) error {
			return tailFiles(ctx, files, n, follow, emit)
		})
	}
	return errs.Config("LOCAL_FILE_BAD_VERB", "local-file supports: logs, ls, doctor")
}

// configFiles reads the `files` allow-list from the env block.
func configFiles(raw map[string]any) []string {
	xs, ok := raw["files"].([]any)
	if !ok {
		return nil
	}
	files := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok && s != "" {
			files = append(files, s)
		}
	}
	return files
}

func hasFollow(args []string) bool {
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			return true
		}
	}
	return false
}

// tailLines parses --tail/-n (default 200).
func tailLines(args []string) int {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--tail" || args[i] == "-n" {
			if n, err := strconv.Atoi(args[i+1]); err == nil && n >= 0 {
				return n
			}
		}
	}
	return 200
}

// tailFiles emits the last n lines of each file (with a `==> file <==` header
// when there is more than one), then, if follow, polls for appended lines until
// ctx is cancelled.
func tailFiles(ctx context.Context, files []string, n int, follow bool, emit func(string) error) error {
	header := len(files) > 1
	offsets := make(map[string]int64, len(files))
	for _, f := range files {
		if header {
			if err := emit("==> " + f + " <=="); err != nil {
				return err
			}
		}
		lines, size, err := lastLines(f, n)
		if err != nil {
			return err
		}
		for _, l := range lines {
			if err := emit(l); err != nil {
				return err
			}
		}
		offsets[f] = size
	}
	if !follow {
		return nil
	}
	return followFiles(ctx, files, offsets, emit)
}

// lastLines returns the last n lines of path and the file's current size (the
// follow offset). It reads backwards in chunks so it never loads more than ~n
// lines worth of bytes.
func lastLines(path string, n int) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, errs.General("LOCAL_FILE_OPEN", err.Error())
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, errs.General("LOCAL_FILE_STAT", err.Error())
	}
	size := fi.Size()
	if n == 0 || size == 0 {
		return nil, size, nil
	}
	const chunk = 64 * 1024
	var data []byte
	pos := size
	for pos > 0 && bytes.Count(data, []byte{'\n'}) <= n {
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil, 0, errs.General("LOCAL_FILE_READ", err.Error())
		}
		data = append(buf, data...)
	}
	lines := splitLines(data)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, size, nil
}

// splitLines splits a byte slice into lines, dropping a single trailing newline
// so a file ending in "\n" doesn't yield a phantom empty last line.
func splitLines(data []byte) []string {
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// followFiles polls each file for appended bytes and emits any complete lines,
// carrying a partial trailing line across polls. Returns nil on ctx cancel.
func followFiles(ctx context.Context, files []string, offsets map[string]int64, emit func(string) error) error {
	partial := make(map[string][]byte, len(files))
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, f := range files {
				if err := pump(f, offsets, partial, emit); err != nil {
					return err
				}
			}
		}
	}
}

// pump reads bytes appended to path since its last offset and emits the complete
// lines, keeping any trailing partial line for the next poll. A shrunken file
// (truncation/rotation) resets the offset to 0.
func pump(path string, offsets map[string]int64, partial map[string][]byte, emit func(string) error) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil // transient (e.g. mid-rotation); try again next tick
	}
	size := fi.Size()
	off := offsets[path]
	if size < off {
		off, partial[path] = 0, nil
	}
	if size == off {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, size-off)
	nRead, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil
	}
	offsets[path] = off + int64(nRead)
	data := append(partial[path], buf[:nRead]...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		if err := emit(string(data[:i])); err != nil {
			return err
		}
		data = data[i+1:]
	}
	partial[path] = append([]byte(nil), data...)
	return nil
}
