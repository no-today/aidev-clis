package exec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Local runs the Spec on this host via os/exec.
type Local struct{}

func (Local) Run(ctx context.Context, s Spec, onLine func(string) error) error {
	if len(s.Argv) == 0 {
		return errs.General("EXEC_NO_ARGV", "empty argv")
	}
	if _, err := osexec.LookPath(s.Argv[0]); err != nil {
		return errs.Config("EXEC_BINARY_MISSING",
			fmt.Sprintf("%q not found on PATH (install it to use this adapter)", s.Argv[0]))
	}
	cmd := osexec.CommandContext(ctx, s.Argv[0], s.Argv[1:]...)
	cmd.Env = append(os.Environ(), s.Env...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errs.General("EXEC_PIPE", err.Error())
	}
	// Only the first ~300 bytes of stderr ever surface (see truncate below), so
	// cap what we retain — a chatty failing subprocess must not balloon memory.
	var stderr capWriter
	stderr.cap = 4096
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return errs.General("EXEC_START", err.Error())
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if err := onLine(sc.Text()); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait() // reap the child; avoid a leaked process
			return err
		}
	}
	// A scanner error (e.g. a line exceeding the 1 MiB cap) would otherwise end
	// the stream silently and look like clean EOF — surface it instead.
	if err := sc.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errs.General("EXEC_SCAN", fmt.Sprintf("reading %s output: %v", s.Argv[0], err))
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil { // killed by deadline/cancellation, not a remote failure
			return errs.Timeout("EXEC_TIMEOUT", ctx.Err().Error())
		}
		return errs.Remote("EXEC_FAILED",
			fmt.Sprintf("%s exited with error: %v: %s", s.Argv[0], err, truncate(stderr.String(), 300)))
	}
	return nil
}

// capWriter is an io.Writer that retains at most cap bytes and silently drops
// the rest — enough context for an error message, bounded memory under a
// runaway subprocess.
type capWriter struct {
	buf strings.Builder
	cap int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.cap - w.buf.Len(); room > 0 {
		if room < len(p) {
			w.buf.Write(p[:room])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil // report full write so the child never blocks on a full pipe
}

func (w *capWriter) String() string { return w.buf.String() }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
