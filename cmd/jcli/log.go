package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/jcli"
)

func logCmd() *cobra.Command {
	var f targetFlags
	var build int
	var follow bool
	var output string
	c := &cobra.Command{
		Use:   "log <service>",
		Short: "Print a build's console log",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if output != "json" && output != "raw" {
				err := errs.Config("UNSUPPORTED_OUTPUT", "jcli supports --output json|raw")
				auditRead("", err) // target not resolved yet — still audit, never a silent path
				return err
			}
			plain := output == "raw"
			err := func() error {
				cl, job, targetName, _, err := resolveTarget(f.target, first(args), f.job, f.group)
				if err != nil {
					auditRead(targetName, err)
					return err
				}
				ctx := context.Background()
				n, err := cl.ResolveBuild(ctx, job, build)
				if err != nil {
					auditRead(targetName, err)
					return err
				}
				// `log` is a read: exactly one terminal line, reflecting the FINAL
				// outcome. The console fetch can still fail after ResolveBuild
				// succeeds, so audit AFTER it (mirroring status/params), not before.
				fetchErr := func() error {
					if follow {
						return streamConsole(ctx, cl, job, n, plain)
					}
					txt, err := cl.Console(ctx, job, n)
					if err != nil {
						return err
					}
					if plain {
						// Raw console text, no envelope — for human reading / piping.
						_, err := fmt.Fprint(os.Stdout, txt)
						return err
					}
					return emit(map[string]any{"job": job, "build": n, "console": txt})
				}()
				auditRead(targetName, fetchErr)
				return fetchErr
			}()
			if err != nil && plain {
				e := errs.From(err)
				_ = envelope.WriteRawError(os.Stdout, e.Code, e.Message)
				os.Exit(e.Exit)
			}
			return err
		},
	}
	f.bind(c)
	c.Flags().IntVar(&build, "build", 0, "build number (default: last)")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "stream the console live")
	c.Flags().StringVar(&output, "output", "json", "output format: json|raw")
	_ = c.RegisterFlagCompletionFunc("output", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "raw"}, cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// streamConsole polls progressive console output until Jenkins reports no more
// data (or ctx is cancelled). Each line is written as NDJSON, or as raw text
// when plain is set (for human reading / piping).
func streamConsole(ctx context.Context, cl *jcli.Client, job string, build int, plain bool) error {
	var s *envelope.Stream
	if !plain {
		s = envelope.NewStream(os.Stdout)
	}
	start, count := 0, 0
	for {
		txt, next, more, err := cl.ConsoleProgressive(ctx, job, build, start)
		if err != nil {
			return err
		}
		if txt != "" {
			if plain {
				if _, err := fmt.Fprint(os.Stdout, txt); err != nil {
					return err
				}
			} else {
				for _, line := range strings.Split(strings.TrimRight(txt, "\n"), "\n") {
					_ = s.Record(map[string]any{"message": line})
					count++
				}
			}
		}
		start = next
		if !more {
			if plain {
				return nil
			}
			return s.EndCtx(dctx(), count)
		}
		select {
		case <-ctx.Done():
			if plain {
				return nil
			}
			return s.EndCtx(dctx(), count)
		case <-time.After(time.Second):
		}
	}
}
