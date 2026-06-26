package cmd

import (
	"github.com/spf13/cobra"

	"github.com/vnazarenko/th-cli/internal/api"
	"github.com/vnazarenko/th-cli/internal/output"
)

// newVersionCmd builds the `th-cli version` subcommand. It reports the build
// metadata (api.Version / api.Commit), which the Makefile injects via -ldflags
// and which also feeds the User-Agent header — one source of truth. Output is
// JSON, matching the CLI's JSON-only contract so agents parse it the same way
// they parse data results.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print th-cli version and build commit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.WriteJSON(cmd.OutOrStdout(), map[string]string{
				"version": api.Version,
				"commit":  api.Commit,
			})
		},
	}
}
