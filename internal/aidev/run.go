package aidev

import (
	"io"
	"os"

	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Run executes discovery from cwd and writes to w, returning the exit code.
// output is "json" (default, the {data} envelope) or "raw" (human summary).
func Run(w io.Writer, cwd, output string, pretty bool) int {
	if output != "" && output != "json" && output != "raw" {
		e := errs.Config("UNSUPPORTED_OUTPUT", "aidev supports --output json|raw")
		audit.Begin(audit.Record{Tool: "aidev", Command: audit.CommandLine(os.Args)}).Finish(e, nil)
		_ = envelope.WriteError(w, e.Code, e.Message, pretty)
		return e.Exit
	}
	raw := output == "raw"

	inv, err := Build(ResolveScene(cwd))
	if err != nil {
		e := errs.From(err)
		audit.Begin(audit.Record{Tool: "aidev", Command: audit.CommandLine(os.Args)}).Finish(e, nil)
		if raw {
			_ = envelope.WriteRawError(w, e.Code, e.Message)
		} else {
			_ = envelope.WriteError(w, e.Code, e.Message, pretty)
		}
		return e.Exit
	}

	audit.Begin(audit.Record{Tool: "aidev", Command: audit.CommandLine(os.Args)}).Finish(nil, nil)
	if raw {
		RenderRaw(w, inv)
	} else {
		_ = envelope.WriteData(w, inv, nil, pretty)
	}
	return errs.ExitOK
}
