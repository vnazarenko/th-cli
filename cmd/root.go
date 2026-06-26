// Package cmd implements the `th-cli` CLI command tree (Cobra) for trendHERO's
// public API. The root command wires global/persistent flags and a central
// Execute() entry point that returns a process exit code.
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/vnazarenko/th-cli/internal/config"
	"github.com/vnazarenko/th-cli/internal/output"
)

// Persistent flag values shared across the command tree. They are bound on the
// root command and consumed by subcommands (config resolution lands in Task 4).
var (
	flagToken   string
	flagBaseURL string
	flagConfig  string
)

// newRootCmd builds the root `th-cli` command. It is a function (not a package
// global) so tests can construct an isolated command tree per invocation.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "th-cli",
		Short: "trendHERO CLI — query Instagram influencer analytics via the public API",
		Long: "th-cli is a thin, agent-friendly wrapper around trendHERO's public API.\n" +
			"It emits JSON to stdout and encodes failure classes as exit codes so\n" +
			"automation can branch without parsing text.",
		// The root takes no positional args of its own — subcommands own those.
		// NoArgs makes a stray/unknown bare argument an error (exit 1) rather
		// than silently printing help and succeeding.
		Args: cobra.NoArgs,
		// A RunE makes the root "runnable" so NoArgs is actually validated
		// (cobra short-circuits arg validation for non-runnable commands).
		// With no args it just prints help and succeeds; subcommands, once
		// added, are resolved before this runs.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		// We render our own errors/usage and map them to exit codes; let
		// Execute() own that rather than Cobra printing+exiting on its own.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flagToken, "token", "", "trendHERO AccessToken (overrides TRENDHERO_TOKEN)")
	pf.StringVar(&flagBaseURL, "base-url", "", "API host override (default https://trendhero.io; overrides TRENDHERO_BASE_URL)")
	pf.StringVar(&flagConfig, "config", "", "config file path (default ~/.config/th-cli/config.yaml)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newTopProfilesCmd())
	root.AddCommand(newReportCmd())

	return root
}

// currentFlags snapshots the root command's persistent flag values into a
// config.Flags so subcommands can resolve the effective configuration. It is the
// single bridge between Cobra's flag binding and the config package.
func currentFlags() config.Flags {
	return config.Flags{
		Token:   flagToken,
		BaseURL: flagBaseURL,
		Config:  flagConfig,
	}
}

// Execute builds the command tree, runs it against the process args, and
// returns a process exit code.
func Execute() int {
	return run(os.Args[1:])
}

// run executes the root command with an explicit argument list. It is the
// testable seam behind Execute() — tests drive it with crafted args.
//
// The command runs under a context cancelled on SIGINT/SIGTERM, so a long
// `report get --wait` poll aborts promptly on Ctrl-C (the abort surfaces as a
// network/timeout-class error, exit 5).
//
// A returned error is rendered as a JSON `{"error":...,"hint":...}` envelope to
// stderr and mapped to its failure-class exit code via output.ExitCode: 0 ok,
// 1 usage/generic, 2 auth, 3 forbidden, 4 not-found, 5 network/timeout,
// 6 validation, 7 unavailable.
func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd()
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		_ = output.WriteError(root.ErrOrStderr(), err)
		return output.ExitCode(err)
	}
	return 0
}
