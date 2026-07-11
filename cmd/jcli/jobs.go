package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/jcli"
)

func jobsCmd() *cobra.Command {
	var target string
	var sync bool
	c := &cobra.Command{
		Use:   "jobs",
		Short: "List Jenkins jobs (local cache; --sync refreshes from Jenkins)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			e, cfg, err := loadTarget(target)
			if err != nil {
				auditRead(target, err)
				return err
			}
			if !sync {
				cache, err := jcli.LoadJobsCache(e.Name)
				auditRead(e.Name, err)
				if err != nil {
					return err
				}
				return emit(cache)
			}
			cl, err := jcli.NewClient(cfg)
			if err != nil {
				auditRead(e.Name, err)
				return err
			}
			jobs, err := cl.WalkJobs(context.Background())
			if err != nil {
				auditRead(e.Name, err)
				return err
			}
			cache := &jcli.JobsCache{Target: e.Name, BaseURL: cfg.BaseURL, SyncedAt: nowRFC3339(), Jobs: jobs}
			if err := cache.Save(); err != nil {
				auditRead(e.Name, err)
				return err
			}
			auditRead(e.Name, nil)
			return emit(cache)
		},
	}
	c.Flags().StringVar(&target, "target", "", "configured target")
	c.Flags().BoolVar(&sync, "sync", false, "refresh the cache from Jenkins (recursive)")
	return c
}
