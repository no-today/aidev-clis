package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/jcli"
)

// Check is one doctor stage result. The doctor's envelope `data` is a flat list
// of these (overall health is the exit code, not a redundant top-level field).
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func doctorCmd() *cobra.Command {
	var target string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose a configured target: config + Jenkins auth",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			e, cfg, err := loadTarget(target)
			if err != nil {
				auditRead(target, err)
				return err // can't identify the target at all → plain {error}
			}
			var checks []Check
			cl, err := jcli.NewClient(cfg)
			if err != nil {
				ce := errs.From(err)
				checks = append(checks, Check{Name: "config", OK: false, Detail: ce.Message})
				return finishDoctor(e.Name, checks, ce.Exit, ce)
			}
			checks = append(checks, Check{Name: "config", OK: true, Detail: "base_url " + cfg.BaseURL})

			if user, err := cl.Whoami(context.Background()); err != nil {
				ce := errs.From(err)
				checks = append(checks, Check{Name: "auth", OK: false, Detail: ce.Message})
				return finishDoctor(e.Name, checks, ce.Exit, ce)
			} else {
				checks = append(checks, Check{Name: "auth", OK: true, Detail: "authenticated as " + user})
			}
			return finishDoctor(e.Name, checks, 0, nil)
		},
	}
	c.Flags().StringVar(&target, "target", "", "configured target")
	return c
}

// finishDoctor emits the checks envelope and records the run; a non-zero exit is
// carried via doctorExit (main() applies it after Execute). failErr carries the
// real stage code (from NewClient/Whoami) so the audit line records the true
// failure; it is a code carrier for the audit line ONLY — the exit code is `exit`,
// NOT failErr.Exit. Fall back to a synthetic DOCTOR_UNHEALTHY only when a failure
// somehow arrived without a classified error.
func finishDoctor(targetName string, checks []Check, exit int, failErr *errs.Error) error {
	doctorExit = exit
	var err error
	if exit != 0 {
		if failErr != nil {
			err = failErr
		} else {
			err = errs.Config("DOCTOR_UNHEALTHY", "doctor reported a failing stage")
		}
	}
	auditRead(targetName, err)
	return emit(checks)
}
