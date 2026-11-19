package main

import (
	"context"

	"github.com/spf13/cobra"
)

func cancelCmd() *cobra.Command {
	var f targetFlags
	var build int
	c := &cobra.Command{
		Use:   "cancel <service>",
		Short: "Cancel a running build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
			op := beginJob(targetName, true, job, nil)
			err = cl.Cancel(ctx, job, n)
			op.Finish(err, map[string]any{"build": n})
			if err != nil {
				return err
			}
			return emit(map[string]any{"job": job, "build": n, "cancelled": true})
		},
	}
	f.bind(c)
	c.Flags().IntVar(&build, "build", 0, "build number (default: last)")
	return c
}
