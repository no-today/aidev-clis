package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

// buildResultMap is the audit result for build/deploy: {build} plus {state} only
// when non-empty (Result is "" while a build runs without --wait).
func buildResultMap(build int, state string) map[string]any {
	m := map[string]any{"build": build}
	if state != "" {
		m["state"] = state
	}
	return m
}

func buildCmd() *cobra.Command {
	var f targetFlags
	var params []string
	var wait bool
	var timeout time.Duration
	c := &cobra.Command{
		Use:   "build <service>",
		Short: "Trigger a Jenkins build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, job, targetName, _, err := resolveTarget(f.target, first(args), f.job, f.group)
			if err != nil {
				auditRead(targetName, err)
				return err
			}
			p, err := parseParams(params)
			if err != nil {
				auditRead(targetName, err)
				return err
			}
			// A hung Jenkins build must not block the agent forever: --wait polls
			// under a deadline (mirrors deploy). The deadline fires as a Timeout
			// (exit 4) from Wait. Harmless without --wait (Trigger returns fast).
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			started := time.Now()
			op := beginJob(targetName, true, job, p)
			res, err := cl.Trigger(ctx, job, p)
			if err != nil {
				op.Finish(err, nil)
				return err
			}
			triggered := res.Build // preserve for audit even if Wait fails at the HTTP level
			dg.Logf(1, "triggered build #%d in %s", res.Build, time.Since(started).Round(time.Millisecond))
			if wait {
				var werr error
				res, werr = cl.Wait(ctx, job, res.Build)
				if res.Build == 0 { // Wait failed before it could report a number → keep the triggered one
					res.Build = triggered
				}
				if werr == nil {
					dg.Logf(1, "build #%d finished result=%s in %s", res.Build, res.Result, time.Since(started).Round(time.Millisecond))
					res.Artifacts, werr = cl.Artifacts(ctx, job, res.Build)
				}
				err = werr
			}
			op.Finish(err, buildResultMap(res.Build, res.Result))
			if err != nil {
				return err
			}
			return emit(res)
		},
	}
	f.bind(c)
	c.Flags().StringArrayVar(&params, "param", nil, "build param k=v (repeatable)")
	c.Flags().BoolVar(&wait, "wait", false, "wait for the build to finish")
	c.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "bound the --wait poll")
	return c
}
