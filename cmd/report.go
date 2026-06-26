package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/vnazarenko/th-cli/internal/api"
	"github.com/vnazarenko/th-cli/internal/config"
	"github.com/vnazarenko/th-cli/internal/output"
)

// newReportCmd builds the `th-cli report` parent command grouping the report
// subcommands. Reports are Bearer-guarded server-side, so every subcommand
// requires a token (enforced via config.RequireToken). The parent itself is not
// a data command — with no subcommand it just prints help.
func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Order and fetch trendHERO influencer reports (token required)",
		Long: "Work with trendHERO influencer reports.\n\n" +
			"Reports require an AccessToken (set TRENDHERO_TOKEN or pass --token).\n" +
			"Use `report get <username>` to fetch a report (add --wait to poll until\n" +
			"it is ready) and `report order <username>` to order a new one (paid).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newReportGetCmd())
	cmd.AddCommand(newReportOrderCmd())

	return cmd
}

// newReportGetCmd builds `th-cli report get <username>`. It fetches a single report
// and passes the JSON straight through to stdout. With --wait it polls until the
// report reaches a terminal status (ready/recollecting/impossibleButReady or the
// terminal-failed impossible); reaching any terminal status — impossible
// included — prints the report JSON and exits 0, leaving the agent to read
// report.status from the blob.
func newReportGetCmd() *cobra.Command {
	var (
		wait     bool
		timeout  time.Duration
		interval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "get <username>",
		Short: "Fetch an influencer report by Instagram username",
		Long: "Fetch the report for <username> and emit it as JSON to stdout.\n\n" +
			"Without --wait the current report is returned as-is (it may still be\n" +
			"`collecting`). With --wait the command polls until the report reaches a\n" +
			"terminal status and then prints it. A terminal `impossible` status is\n" +
			"still a success (exit 0): inspect report.status in the output.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			cfg, err := config.Resolve(currentFlags())
			if err != nil {
				return err
			}
			// Reports are Bearer-guarded: a token is mandatory here (unlike
			// top-profiles). RequireToken maps to exit 2 (auth) when absent.
			if err := cfg.RequireToken(); err != nil {
				return err
			}

			client, err := api.New(cfg)
			if err != nil {
				return err
			}

			var raw json.RawMessage
			if wait {
				raw, err = client.WaitForReport(cmd.Context(), username, interval, timeout)
			} else {
				raw, err = client.GetReport(cmd.Context(), username)
			}
			if err != nil {
				return err
			}
			return output.WriteJSON(cmd.OutOrStdout(), raw)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&wait, "wait", false,
		"poll until the report reaches a terminal status (ready/recollecting/impossibleButReady/impossible)")
	f.DurationVar(&timeout, "timeout", api.DefaultPollTimeout, "max time to wait when --wait is set")
	f.DurationVar(&interval, "interval", api.DefaultPollInterval, "poll interval when --wait is set")

	return cmd
}

// newReportOrderCmd builds `th-cli report order <username>`. Ordering a report is a
// PAID action — it spends account credits — so it is guarded: the command
// refuses unless the caller opts in with --confirm or TRENDHERO_ALLOW_WRITES=1.
// Without that opt-in it returns a plain error (exit 1) and makes NO API call,
// so an accidental invocation can never spend credits. With --wait it polls
// after ordering until the report reaches a terminal status (impossible
// included → exit 0); the agent reads report.status from the printed JSON.
func newReportOrderCmd() *cobra.Command {
	var (
		confirm  bool
		wait     bool
		timeout  time.Duration
		interval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "order <username>",
		Short: "Order a new influencer report (PAID — spends credits)",
		Long: "Order (pay for) a fresh report for <username>.\n\n" +
			"This SPENDS account credits, so it is guarded: pass --confirm (or set\n" +
			"TRENDHERO_ALLOW_WRITES=1) to actually place the order. Without that\n" +
			"opt-in the command refuses and makes no API call.\n\n" +
			"Ordering returns a `collecting` report. Add --wait to poll until the\n" +
			"report reaches a terminal status and print the final report JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			cfg, err := config.Resolve(currentFlags())
			if err != nil {
				return err
			}
			// Reports are Bearer-guarded: a token is mandatory (exit 2 if absent).
			if err := cfg.RequireToken(); err != nil {
				return err
			}

			// Paid-action guard: refuse before touching the network unless the
			// caller explicitly opted in. A plain error maps to exit 1 (generic).
			if !confirm && !config.WritesAllowed() {
				return fmt.Errorf("ordering a report for %q spends account credits; "+
					"re-run with --confirm (or set %s=1) to proceed",
					username, config.EnvAllowWrites)
			}

			client, err := api.New(cfg)
			if err != nil {
				return err
			}

			// Place the (paid) order. Its response is a freshly `collecting`
			// report unless --wait supersedes it with the polled terminal one.
			raw, err := client.OrderReport(cmd.Context(), username)
			if err != nil {
				return err
			}
			if wait {
				raw, err = client.WaitForReport(cmd.Context(), username, interval, timeout)
				if err != nil {
					return err
				}
			}
			return output.WriteJSON(cmd.OutOrStdout(), raw)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&confirm, "confirm", false,
		"confirm the paid order (required unless "+config.EnvAllowWrites+"=1)")
	f.BoolVar(&wait, "wait", false,
		"after ordering, poll until the report reaches a terminal status")
	f.DurationVar(&timeout, "timeout", api.DefaultPollTimeout, "max time to wait when --wait is set")
	f.DurationVar(&interval, "interval", api.DefaultPollInterval, "poll interval when --wait is set")

	return cmd
}
