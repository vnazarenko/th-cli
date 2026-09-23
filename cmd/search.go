package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vnazarenko/th-cli/internal/api"
	"github.com/vnazarenko/th-cli/internal/config"
	"github.com/vnazarenko/th-cli/internal/output"
)

// Paging bounds, mirrored from the server (Search::Users::ApiService::MIN_SIZE /
// MAX_SIZE). They are enforced locally so an out-of-range value fails instantly
// instead of costing a round trip for a 422 — the server rejects rather than
// clamping, so a client that guesses gets nothing back either way.
const (
	minSize = 1
	maxSize = 50
	// defaultSize matches the server's own default (UI parity). Every result is
	// charged, so this is a cost knob: 50 costs ~50x what 1 does.
	defaultSize = 15
)

// stdinSentinel is the --filters-json value that means "read from stdin".
const stdinSentinel = "-"

// genderCodes maps the human words a caller would reach for onto the numeric
// codes the index stores. The server drops the WHOLE gender filter unless every
// element is one of 0/1/2/3 (and the API 422s rather than let that happen), so
// translating here is what keeps `--gender female` from being a silent no-op.
var genderCodes = map[string]int{
	"none":   0, // no gender detected — the UI counts this as a "brand" account
	"male":   1,
	"female": 2,
	"brand":  3, // gender could not be determined
}

// contactSources are the accepted --with-contacts values, matching
// Users::QueryBuilder::TYPES_OF_CONTACTS_FILTER. Anything else is dropped
// server-side without an error, so it is validated here.
var contactSources = map[string]bool{
	"biography_contacts": true, // an email/phone found in the bio text
	"trendhero_contacts": true, // a contact trendHERO holds for the account
}

// newSearchCmd builds `th-cli search`. Search is Bearer-guarded and PAID, but —
// unlike `report order` — it carries no --confirm guard: a single search is
// cheap, agents run many of them, and a prompt on every call would train the
// caller to pass --confirm reflexively. The cost is documented instead, here
// and in the skill.
//
// There is deliberately no --all / --auto-page flag. Paging spends per-hit
// credits, so a convenience flag that quietly issues N billed calls could drain
// a customer's balance from one command line.
func newSearchCmd() *cobra.Command {
	var (
		keywords     []string
		followersMin int64
		followersMax int64
		erMin        float64
		erMax        float64
		countries    []string
		languages    []string
		categories   []string
		gender       string
		verified     bool
		withContacts []string
		filtersJSON  string
		page         int
		size         int
	)

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search (discover) Instagram accounts (PAID — spends credits per result)",
		Long: "Discover Instagram accounts matching a filter set.\n\n" +
			"⚠️ EVERY CALL SPENDS CREDITS — a flat charge per search plus one per\n" +
			"result returned. --size is therefore a direct cost multiplier, and\n" +
			"there is no --all flag: page explicitly, one billed call at a time.\n\n" +
			"Pages are 0-INDEXED: the first page is --page 0 (the default).\n" +
			"--size must be 1-50; anything else is rejected locally, and the server\n" +
			"rejects it with a 422 rather than clamping.\n\n" +
			"The flags below cover the common filters. For the full surface —\n" +
			"including the premium audience filters, sorting and location\n" +
			"filters — pass --filters-json with a file (or `-` for stdin); its\n" +
			"keys are merged OVER anything the flags set.\n\n" +
			"Output is the raw API response: {\"results\": [...], \"pagination\": {...}}.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := buildSearchBody(searchFlags{
				keywords:        keywords,
				followersMin:    followersMin,
				followersMinSet: cmd.Flags().Changed("followers-min"),
				followersMax:    followersMax,
				followersMaxSet: cmd.Flags().Changed("followers-max"),
				erMin:           erMin,
				erMinSet:        cmd.Flags().Changed("er-min"),
				erMax:           erMax,
				erMaxSet:        cmd.Flags().Changed("er-max"),
				countries:       countries,
				languages:       languages,
				categories:      categories,
				gender:          gender,
				verified:        verified,
				verifiedSet:     cmd.Flags().Changed("verified"),
				withContacts:    withContacts,
				page:            page,
				size:            size,
			}, filtersJSON, cmd.InOrStdin())
			if err != nil {
				return err
			}

			cfg, err := config.Resolve(currentFlags())
			if err != nil {
				return err
			}
			// Search is Bearer-guarded: a token is mandatory. RequireToken maps
			// to exit 2 (auth) when absent — and short-circuits before any HTTP
			// call, so a tokenless invocation can never reach a billed endpoint.
			if err := cfg.RequireToken(); err != nil {
				return err
			}

			client, err := api.New(cfg)
			if err != nil {
				return err
			}

			raw, err := client.SearchAccounts(cmd.Context(), body)
			if err != nil {
				return err
			}
			return output.WriteJSON(cmd.OutOrStdout(), raw)
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&keywords, "keywords", nil,
		"phrases matched against username/full name/bio (repeatable or comma-separated; any may match)")
	f.Int64Var(&followersMin, "followers-min", 0, "minimum follower count")
	f.Int64Var(&followersMax, "followers-max", 0, "maximum follower count")
	f.Float64Var(&erMin, "er-min", 0, "minimum engagement rate, in percent (e.g. 2.5)")
	f.Float64Var(&erMax, "er-max", 0, "maximum engagement rate, in percent")
	f.StringSliceVar(&countries, "country", nil,
		"ISO country code(s) the ACCOUNT is in (repeatable or comma-separated)")
	f.StringSliceVar(&languages, "language", nil, "3-letter language code(s) the account posts in, e.g. spa,eng")
	f.StringSliceVar(&categories, "category", nil, "Instagram business category name(s)")
	f.StringVar(&gender, "gender", "", "account gender: "+genderHint())
	f.BoolVar(&verified, "verified", false,
		"restrict to verified accounts (--verified=false restricts to UNverified; omit for either)")
	f.StringSliceVar(&withContacts, "with-contacts", nil,
		"restrict to accounts with contacts: "+contactsHint()+" (repeatable; OR)")
	f.StringVar(&filtersJSON, "filters-json", "",
		"path to a JSON file with the full search_params object, or `-` to read stdin; merged OVER the flags above")
	f.IntVar(&page, "page", 0, "page to fetch — 0-INDEXED, so the first page is 0")
	f.IntVar(&size, "size", defaultSize,
		fmt.Sprintf("results per page (%d-%d) — each result is CHARGED", minSize, maxSize))

	return cmd
}

// searchFlags is the flag snapshot buildSearchBody turns into a request body.
// The *Set fields report whether the user actually passed the flag: a numeric
// flag's zero value is a legitimate bound (`--followers-min 0` is a no-op but
// `--er-min 0` is meaningful) and `--verified=false` is a real filter, so an
// unset flag cannot be inferred from its value alone.
type searchFlags struct {
	keywords        []string
	followersMin    int64
	followersMinSet bool
	followersMax    int64
	followersMaxSet bool
	erMin           float64
	erMinSet        bool
	erMax           float64
	erMaxSet        bool
	countries       []string
	languages       []string
	categories      []string
	gender          string
	verified        bool
	verifiedSet     bool
	withContacts    []string
	page            int
	size            int
}

// buildSearchBody validates the flags and assembles the JSON request body.
//
// Everything it can catch locally, it catches locally: an out-of-range page or
// size, an unknown gender word, an unknown contact source, unreadable or
// malformed --filters-json. All of those are plain errors (exit 1) raised
// BEFORE the token is even resolved, so a mistyped command never reaches a
// billed endpoint.
//
// --filters-json is merged OVER the individual flags, key by key at the top
// level of search_params: a JSON file naming `follower_count` wins outright
// over --followers-min/--followers-max rather than being blended with it. That
// is the only merge rule that stays predictable once the JSON side can express
// filters the flags cannot.
func buildSearchBody(f searchFlags, filtersJSON string, stdin io.Reader) (json.RawMessage, error) {
	// Bounds first — the cheapest possible failure for the most expensive
	// possible mistake.
	if f.page < 0 {
		return nil, fmt.Errorf("invalid --page %d: pages are 0-indexed, so the first page is 0", f.page)
	}
	if f.size < minSize || f.size > maxSize {
		return nil, fmt.Errorf(
			"invalid --size %d: must be between %d and %d (the API rejects anything else with a 422 rather than clamping)",
			f.size, minSize, maxSize)
	}

	params, err := searchParamsFromFlags(f)
	if err != nil {
		return nil, err
	}

	overrides, err := loadFiltersJSON(filtersJSON, stdin)
	if err != nil {
		return nil, err
	}
	for k, v := range overrides {
		params[k] = v
	}

	// After the merge, so a code arriving through --filters-json is checked
	// exactly like one from --language.
	if err := validateLanguages(params["languages"]); err != nil {
		return nil, err
	}

	body := map[string]any{
		"page": f.page,
		"size": f.size,
	}
	// An absent search_params means "match everything", which is a legitimate
	// (and expensive) request. Send it only when it is empty by intent rather
	// than sending `{}` — the server treats both identically, and omitting the
	// key keeps the emitted body honest about what was asked for.
	if len(params) > 0 {
		body["search_params"] = params
	}

	return json.Marshal(body)
}

// searchParamsFromFlags maps the ergonomic flags onto the API's filter names.
func searchParamsFromFlags(f searchFlags) (map[string]any, error) {
	params := map[string]any{}

	if len(f.keywords) > 0 {
		params["keywords"] = f.keywords
	}
	if r := rangeFilter(
		f.followersMin, f.followersMinSet,
		f.followersMax, f.followersMaxSet,
	); r != nil {
		params["follower_count"] = r
	}
	if r := rangeFilter(f.erMin, f.erMinSet, f.erMax, f.erMaxSet); r != nil {
		params["general_er"] = r
	}
	if len(f.languages) > 0 {
		langs := make([]string, 0, len(f.languages))
		for _, l := range f.languages {
			langs = append(langs, strings.ToLower(strings.TrimSpace(l)))
		}
		params["languages"] = langs
	}
	if len(f.categories) > 0 {
		params["instagram_category"] = f.categories
	}

	// --country maps onto the `locations` filter's include half. The server
	// splits that list on `should_be_included`, and an entry without the flag
	// is a 422, so it is always written out explicitly.
	if len(f.countries) > 0 {
		locations := make([]map[string]any, 0, len(f.countries))
		for _, c := range f.countries {
			locations = append(locations, map[string]any{
				"type":               "country",
				"country":            strings.ToUpper(strings.TrimSpace(c)),
				"should_be_included": true,
			})
		}
		params["locations"] = locations
	}

	if f.gender != "" {
		code, ok := genderCodes[strings.ToLower(strings.TrimSpace(f.gender))]
		if !ok {
			return nil, fmt.Errorf("invalid --gender %q: expected one of %s", f.gender, genderHint())
		}
		params["gender"] = []int{code}
	}

	// Only set when the flag was actually passed: `is_verified: false` is a
	// real filter (unverified accounts only), not "no opinion".
	if f.verifiedSet {
		params["is_verified"] = f.verified
	}

	if len(f.withContacts) > 0 {
		sources := make([]string, 0, len(f.withContacts))
		for _, s := range f.withContacts {
			s = strings.ToLower(strings.TrimSpace(s))
			if !contactSources[s] {
				return nil, fmt.Errorf("invalid --with-contacts %q: expected one of %s", s, contactsHint())
			}
			sources = append(sources, s)
		}
		params["with_contacts"] = sources
	}

	return params, nil
}

// rangeFilter builds a `{gte, lte}` object from a min/max pair, returning nil
// when neither bound was set. It is generic over the numeric flag types so the
// integer (follower count) and float (engagement rate) pairs share one rule.
func rangeFilter[T int64 | float64](minVal T, minSet bool, maxVal T, maxSet bool) map[string]any {
	if !minSet && !maxSet {
		return nil
	}
	r := map[string]any{}
	if minSet {
		r["gte"] = minVal
	}
	if maxSet {
		r["lte"] = maxVal
	}
	return r
}

// loadFiltersJSON reads and parses the --filters-json source. It returns nil
// when the flag was not passed.
//
// The parsed value must be a JSON OBJECT. The server makes the same
// distinction — an absent search_params means "match everything", but a
// non-object (an array, a bare string) is a 422 with nothing deducted, because
// reading a broken serialisation as "no filters" used to run and BILL a full
// unfiltered search. Catching it here means the caller never pays for it.
func loadFiltersJSON(source string, stdin io.Reader) (map[string]any, error) {
	if source == "" {
		return nil, nil
	}

	var (
		raw []byte
		err error
	)
	if source == stdinSentinel {
		raw, err = io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read --filters-json from stdin: %w", err)
		}
	} else {
		raw, err = os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read --filters-json: %w", err)
		}
	}

	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("--filters-json %s is empty: expected a JSON object of search_params", describeSource(source))
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("--filters-json %s is not valid JSON: %w", describeSource(source), err)
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(
			"--filters-json %s must be a JSON object of search_params (e.g. {\"follower_count\":{\"gte\":10000}}), not %s",
			describeSource(source), jsonKind(parsed))
	}
	return obj, nil
}

// describeSource renders the --filters-json source for an error message.
func describeSource(source string) string {
	if source == stdinSentinel {
		return "(stdin)"
	}
	return source
}

// jsonKind names the JSON type of a decoded value, for error messages.
func jsonKind(v any) string {
	switch v.(type) {
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	default:
		return "that"
	}
}

// genderHint lists the accepted --gender words, sorted so the message is stable.
func genderHint() string {
	return sortedKeys(genderCodes)
}

// contactsHint lists the accepted --with-contacts values.
func contactsHint() string {
	return sortedKeys(contactSources)
}

func sortedKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// languageCode is the only shape the index stores: three lowercase letters,
// ISO 639-2 bibliographic form (`spa`, `eng`, `fre`).
var languageCode = regexp.MustCompile(`^[a-z]{3}$`)

// twoLetterHints maps the ISO 639-1 codes people reach for first onto the
// codes the index actually carries. Only codes seen in real search results are
// listed — the index uses the bibliographic `fre`, not `fra`, so a guess for a
// language not listed here could easily be the other variant.
var twoLetterHints = map[string]string{
	"en": "eng", "es": "spa", "ca": "cat", "gl": "glg", "fr": "fre",
	"it": "ita", "pt": "por", "tr": "tur", "ja": "jpn",
}

// validateLanguages rejects a language code the index can never match.
//
// ⚠️ This is a money guard, not tidiness. `languages` is an Elasticsearch
// terms filter over three-letter codes, so `es` or `en` matches no account at
// all — and a zero-match search is still a SUCCESS that is billed the flat
// per-search charge. Catching it here costs nothing: no API call is made.
//
// A value that is not a list of strings is left alone — the server rejects a
// wrong shape with an unbilled 422, and restating its rules here would drift.
func validateLanguages(v any) error {
	var codes []string
	switch list := v.(type) {
	case nil:
		return nil
	case []string:
		codes = list
	case []any:
		for _, item := range list {
			code, ok := item.(string)
			if !ok {
				return nil
			}
			codes = append(codes, code)
		}
	default:
		return nil
	}

	for _, code := range codes {
		if languageCode.MatchString(code) {
			continue
		}
		if hint, ok := twoLetterHints[strings.ToLower(code)]; ok {
			return fmt.Errorf("invalid language %q: the index uses 3-letter codes — use %s", code, hint)
		}
		return fmt.Errorf("invalid language %q: the index uses 3-letter lowercase codes such as spa, eng, cat, fre", code)
	}
	return nil
}
