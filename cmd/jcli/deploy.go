package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/jcli"
)

func deployCmd() *cobra.Command {
	var f targetFlags
	var params []string
	var noWait bool
	var timeout time.Duration
	c := &cobra.Command{
		Use:   "deploy <service>",
		Short: "Build, extract values, and run the configured deploy steps",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			service := first(args)
			cl, job, targetName, grp, err := resolveTarget(f.target, service, f.job, f.group)
			if err != nil {
				auditRead(targetName, err)
				return err
			}
			if grp == nil { // --job on a multi-group target that maps to no single group
				err := errs.Config("GROUP_REQUIRED", "deploy needs a group for --job "+job+" (it maps to no single group); pass --group <name>")
				auditRead(targetName, err)
				return err
			}
			p, err := parseParams(params)
			if err != nil {
				auditRead(targetName, err)
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			op := beginJob(targetName, true, job, p)
			res, err := jcli.RunDeploy(ctx, cl, grp, job, service, p, !noWait)
			op.Finish(err, buildResultMap(res.Build, res.Result))
			if err != nil {
				return err
			}
			return emit(res)
		},
	}
	f.bind(c)
	c.Flags().StringArrayVar(&params, "param", nil, "build param k=v (repeatable)")
	c.Flags().BoolVar(&noWait, "no-wait", false, "trigger only (skip wait/extract/steps)")
	c.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "bound the whole deploy")
	return c
}
