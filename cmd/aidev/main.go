package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/aidev"
	"github.com/no-today/aidev-clis/internal/core/buildinfo"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

const long = `With no subcommand: discover what's usable in this workspace (which CLIs apply,
the environment->capability matrix, and apicli apps) — scoped to the active
scene (nearest .aidev.yaml, or AIDEV_SCENE). Read-only; never dispatches.

  config backup   write a timestamped tar.gz of ~/.aidev-clis (*.yaml +
                  credentials/) to <home>/backups/.
  config restore  restore/use an archive; takes a safety backup first unless
                  --no-backup. Aliases: import, use.

Output is the {data}|{error} JSON envelope by default (AI-first); pass
--output raw for a human summary.`

func main() {
	var (
		output    string
		pretty    bool
		configDir string
	)

	root := &cobra.Command{
		Use:           "aidev",
		Short:         "Cross-CLI discovery for the aidev suite (read-only)",
		Version:       buildinfo.Version,
		Long:          long,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if configDir != "" {
				return os.Setenv("AIDEV_CLIS_HOME", configDir)
			}
			return nil
		},
		// No subcommand: run workspace discovery. Run writes the envelope and
		// returns the exit code itself, so exit directly.
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			os.Exit(aidev.Run(os.Stdout, cwd, output, pretty))
			return nil
		},
	}
	root.Flags().StringVar(&output, "output", "json", "output format: json|raw")
	root.Flags().BoolVar(&pretty, "pretty", false, "indent the JSON envelope")
	root.PersistentFlags().StringVar(&configDir, "config-dir", "", "override ~/.aidev-clis (sets AIDEV_CLIS_HOME)")
	_ = root.RegisterFlagCompletionFunc("config-dir", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})
	_ = root.RegisterFlagCompletionFunc("output", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "raw"}, cobra.ShellCompDirectiveNoFileComp
	})

	// config: DisableFlagParsing forwards raw args to internal/aidev.RunConfig,
	// which owns all flag parsing + the {data}|{error} contract. completeConfig
	// fills the completion that DisableFlagParsing switches off.
	configCmd := &cobra.Command{
		Use:                "config <backup|restore> [flags]",
		Short:              "Back up or restore ~/.aidev-clis config",
		DisableFlagParsing: true,
		ValidArgsFunction:  completeConfig,
		RunE: func(_ *cobra.Command, args []string) error {
			os.Exit(aidev.RunConfig(os.Stdout, args))
			return nil
		},
	}
	root.AddCommand(configCmd)

	if err := root.Execute(); err != nil {
		// Only cobra-level failures (unknown command / bad root flag) reach here;
		// discovery and config exit on their own via os.Exit above.
		e := errs.From(err)
		_ = envelope.WriteError(os.Stdout, e.Code, e.Message, pretty)
		os.Exit(e.Exit)
	}
}
