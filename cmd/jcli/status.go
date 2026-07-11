package main

import (
	"context"

	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	var f targetFlags
	var build int
	c := &cobra.Command{
		Use:   "status <service>",
		Short: "Show a build's status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, job, targetName, _, err := resolveTarget(f.target, first(args), f.job, f.group)
			if err != nil {
				auditRead(targetName, err)
				return err
			}
			res, err := cl.Status(context.Background(), job, build)
			auditRead(targetName, err)
			if err != nil {
				return err
			}
			return emit(res)
		},
	}
	f.bind(c)
	c.Flags().IntVar(&build, "build", 0, "build number (default: last)")
	return c
}
