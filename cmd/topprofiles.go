package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vnazarenko/th-cli/internal/api"
	"github.com/vnazarenko/th-cli/internal/config"
	"github.com/vnazarenko/th-cli/internal/output"
)

// countryHint lists the supported `--country` values for error messages and
// flag usage. It mirrors the GetTopProfilesParamsCountry enum in the generated
// client (the single source of truth for validation via its Valid() method).
const countryHint = "US, UA, RU, DE, FR, TR, BR, IT, PL"

// newTopProfilesCmd builds the `th-cli top-profiles` subcommand. This endpoint is
// unauthenticated, so the command resolves config without requiring a token —
// the token is attached only if one happens to be configured. Output is the raw
// JSON list passed straight through to stdout.
func newTopProfilesCmd() *cobra.Command {
	var (
		country string
		typ     string
		year    int
		month   int
	)

	cmd := &cobra.Command{
		Use:   "top-profiles",
		Short: "List trendHERO's ranked top Instagram profiles (no token required)",
		Long: "Fetch the ranked top-profiles list for a country/period.\n\n" +
			"This endpoint is unauthenticated: a token is optional and is sent only\n" +
			"when one is configured. Results are emitted as JSON to stdout.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Pass whether --year/--month were actually provided so an explicit 0
			// (which is otherwise indistinguishable from the unset default) can be
			// rejected rather than silently falling back to the server default.
			params, err := buildTopProfilesParams(country, typ, year, month,
				cmd.Flags().Changed("year"), cmd.Flags().Changed("month"))
			if err != nil {
				return err
			}

			// Token is optional here — resolve config but never RequireToken.
			cfg, err := config.Resolve(currentFlags())
			if err != nil {
				return err
			}

			client, err := api.New(cfg)
			if err != nil {
				return err
			}

			raw, err := client.TopProfiles(cmd.Context(), params)
			if err != nil {
				return err
			}
			return output.WriteJSON(cmd.OutOrStdout(), raw)
		},
	}

	f := cmd.Flags()
	f.StringVar(&country, "country", "", "ISO country filter ("+countryHint+"); default: no filter")
	f.StringVar(&typ, "type", string(api.Absolute), "ranking type: absolute|relative")
	f.IntVar(&year, "year", 0, "ranking window year (defaults server-side to the current year)")
	f.IntVar(&month, "month", 0, "ranking window month 1-12 (defaults server-side to 1)")

	return cmd
}

// buildTopProfilesParams validates the flag values and assembles the typed query
// parameters. An unset year/month and an empty country are omitted so the server
// applies its own defaults / no filter. yearSet/monthSet report whether the user
// actually passed --year/--month: the flag default is 0, so without this an
// explicit --year=0 would be indistinguishable from "unset" and silently fall
// back to the server default. Invalid values produce a plain error, which maps to
// the generic usage exit code (1).
func buildTopProfilesParams(country, typ string, year, month int, yearSet, monthSet bool) (*api.GetTopProfilesParams, error) {
	params := &api.GetTopProfilesParams{}

	if typ != "" {
		t := api.GetTopProfilesParamsType(strings.ToLower(typ))
		if !t.Valid() {
			return nil, fmt.Errorf("invalid --type %q: expected absolute or relative", typ)
		}
		params.Type = &t
	}

	if country != "" {
		c := api.GetTopProfilesParamsCountry(strings.ToUpper(country))
		if !c.Valid() {
			return nil, fmt.Errorf("invalid --country %q: expected one of %s", country, countryHint)
		}
		params.Country = &c
	}

	// Year has no upper bound in the spec, but a non-positive year is meaningless.
	// A negative year is always nonsense; an explicit --year=0 is rejected too
	// (year zero does not exist and "expected a positive year" implies it). An
	// *unset* 0 is the sentinel that lets the server pick the current year.
	if year < 0 || (yearSet && year == 0) {
		return nil, fmt.Errorf("invalid --year %d: expected a positive year", year)
	}
	if year > 0 {
		y := year
		params.Year = &y
	}
	// The spec constrains month to 1-12; reject out-of-range values locally (a
	// clean usage error, exit 1) instead of a server round-trip + 422. An unset 0
	// is the sentinel for the server default; an explicit --month=0 is out of
	// range like any other invalid month.
	if monthSet || month != 0 {
		if month < 1 || month > 12 {
			return nil, fmt.Errorf("invalid --month %d: expected 1-12", month)
		}
		m := month
		params.Month = &m
	}

	return params, nil
}
