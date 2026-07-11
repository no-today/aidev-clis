package main

import (
	"context"

	"github.com/spf13/cobra"
)

func paramsCmd() *cobra.Command {
	var f targetFlags
	c := &cobra.Command{
		Use:   "params <service>",
		Short: "Show a job's (static) parameter definitions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, job, targetName, _, err := resolveTarget(f.target, first(args), f.job, f.group)
			if err != nil {
				auditRead(targetName, err)
				return err
			}
			ps, err := cl.Params(context.Background(), job)
			auditRead(targetName, err)
			if err != nil {
				return err
			}
			return emit(ps)
		},
	}
	f.bind(c)
	return c
}
