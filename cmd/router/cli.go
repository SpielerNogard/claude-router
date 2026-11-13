package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagConfig string
	flagData   string
	flagDebug  bool
	flagNoTUI  bool

	version = "dev" // set via -ldflags at release build
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "claude-router",
		Version: version,
		Short:   "Router between Claude Code and any LLM provider",
		Long: "claude-router accepts Anthropic-shaped requests, forwards them to the\n" +
			"configured provider based on the model name, translates the format if\n" +
			"needed and records token usage and cost.\n\n" +
			"Without a subcommand, the router starts.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runServer(flagConfig, flagData, flagDebug, flagNoTUI)
			return nil
		},
	}
	root.PersistentFlags().StringVar(&flagConfig, "config", defaultConfigPath(), "path to config.toml")
	root.PersistentFlags().StringVar(&flagData, "data", defaultDataDir(), "directory for databases and PID file")
	root.Flags().BoolVar(&flagDebug, "debug", false, "log every request including translation steps")
	root.Flags().BoolVar(&flagNoTUI, "no-tui", false, "show plain log output instead of the terminal UI")

	root.AddCommand(newStopCmd(), newCheckCmd())
	return root
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "stop",
		Short:         "stop the running router",
		Long:          "Reads router.pid from the data directory and sends SIGTERM — same as Ctrl+C.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(flagData)
		},
	}
}

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [provider]",
		Short: "check that providers are reachable and keys are valid",
		Long: "Fetches the model list from every provider with auth = \"key\". This verifies in one\n" +
			"go that base_url is correct, the environment variable is set and the key is valid.\n" +
			"A provider with auth = \"passthrough\" cannot be checked — the router holds no credential.\n\n" +
			"Also runs when the configuration would prevent startup.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			only := ""
			if len(args) == 1 {
				only = args[0]
			}
			return runCheckCmd(flagConfig, only)
		},
	}
}

// failedError is a factual result (a provider failed), not a usage error, so it is not printed.
type failedError struct{ error }

func execute() int {
	if err := newRootCmd().Execute(); err != nil {
		if _, quiet := err.(failedError); !quiet {
			fmt.Fprintln(os.Stderr, paint(styleFail, "Error:"), err)
		}
		return 1
	}
	return 0
}

func main() { os.Exit(execute()) }
