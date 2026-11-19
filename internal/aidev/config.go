package aidev

import (
	"flag"
	"io"
	"os"
	"time"

	"github.com/no-today/aidev-clis/internal/aidev/configarchive"
	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// RunConfig handles `aidev config <subcommand>`. args is everything after
// "config". Returns the process exit code.
func RunConfig(w io.Writer, args []string) int {
	if len(args) == 0 {
		return writeConfigErr(w, errs.Config("MISSING_SUBCOMMAND",
			"usage: aidev config backup|restore [flags]"), false, false)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "backup":
		return runConfigBackup(w, rest)
	case "restore", "import", "use":
		return runConfigRestore(w, rest)
	default:
		return writeConfigErr(w, errs.Config("UNSUPPORTED_SUBCOMMAND",
			"aidev config supports backup, restore"), false, false)
	}
}

func runConfigBackup(w io.Writer, args []string) int {
	fs := flag.NewFlagSet("aidev config backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("output", "json", "output format: json|raw")
	pretty := fs.Bool("pretty", false, "indent the JSON envelope")
	destDir := fs.String("dest-dir", "", "backup directory (default: <home>/backups)")
	if err := fs.Parse(args); err != nil {
		return writeConfigErr(w, errs.Config("BAD_FLAGS", err.Error()), false, false)
	}
	raw := *out == "raw"
	if *out != "json" && *out != "raw" {
		return writeConfigErr(w, errs.Config("UNSUPPORTED_OUTPUT", "supports --output json|raw"), false, false)
	}

	home, err := config.Home()
	if err != nil {
		return writeConfigErr(w, errs.From(err), raw, *pretty)
	}
	result, err := configarchive.Backup(home, *destDir, time.Now())
	if err != nil {
		return writeConfigErr(w, errs.From(err), raw, *pretty)
	}
	audit.Begin(audit.Record{Tool: "aidev", Command: audit.CommandLine(os.Args)}).Finish(nil, nil)
	return writeConfigOK(w, result, raw, *pretty)
}

func runConfigRestore(w io.Writer, args []string) int {
	fs := flag.NewFlagSet("aidev config restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("output", "json", "output format: json|raw")
	pretty := fs.Bool("pretty", false, "indent the JSON envelope")
	backupDir := fs.String("backup-dir", "", "safety backup directory (default: <home>/backups)")
	noBackup := fs.Bool("no-backup", false, "restore without a safety backup first")
	if err := fs.Parse(args); err != nil {
		return writeConfigErr(w, errs.Config("BAD_FLAGS", err.Error()), false, false)
	}
	raw := *out == "raw"
	if *out != "json" && *out != "raw" {
		return writeConfigErr(w, errs.Config("UNSUPPORTED_OUTPUT", "supports --output json|raw"), false, false)
	}
	if fs.NArg() < 1 {
		return writeConfigErr(w, errs.Config("MISSING_ARG", "usage: aidev config restore <archive.tar.gz>"), raw, *pretty)
	}
	archive := fs.Arg(0)

	home, err := config.Home()
	if err != nil {
		return writeConfigErr(w, errs.From(err), raw, *pretty)
	}
	result, err := configarchive.Restore(home, archive, configarchive.RestoreOptions{
		Backup:    !*noBackup,
		BackupDir: *backupDir,
		Now:       time.Now(),
	})
	if err != nil {
		return writeConfigErr(w, errs.From(err), raw, *pretty)
	}
	audit.Begin(audit.Record{Tool: "aidev", Command: audit.CommandLine(os.Args)}).Finish(nil, nil)
	return writeConfigOK(w, result, raw, *pretty)
}

func writeConfigOK(w io.Writer, data any, raw, pretty bool) int {
	if raw {
		RenderConfigRaw(w, data)
	} else {
		_ = envelope.WriteData(w, data, nil, pretty)
	}
	return errs.ExitOK
}

func writeConfigErr(w io.Writer, e *errs.Error, raw, pretty bool) int {
	audit.Begin(audit.Record{Tool: "aidev", Command: audit.CommandLine(os.Args)}).Finish(e, nil)
	if raw {
		_ = envelope.WriteRawError(w, e.Code, e.Message)
	} else {
		_ = envelope.WriteError(w, e.Code, e.Message, pretty)
	}
	return e.Exit
}
