package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/vnazarenko/th-cli/internal/output"
)

// searchOK is a minimal but complete success response: one hit plus the
// pagination envelope the CLI passes straight through.
const searchOK = `{
  "results": [
    {"pk": 173560420, "username": "cristiano", "follower_count": 600000000, "paid": false}
  ],
  "pagination": {"page": 0, "size": 15, "total_pages": 20, "total_results": 48231}
}`

// searchEmpty is the zero-match case: a 200 with an empty array and
// total_pages 0, NOT an error. Page 0 is always valid server-side.
const searchEmpty = `{"results":[],"pagination":{"page":0,"size":15,"total_pages":0,"total_results":0}}`

// runCapturedStdin is runCaptured with a scripted stdin, for `--filters-json -`.
func runCapturedStdin(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)

	code := 0
	if err := root.Execute(); err != nil {
		_ = output.WriteError(&out, err)
		code = output.ExitCode(err)
	}
	return out.String(), code
}

// decodeJSON parses raw JSON into a generic value so two payloads can be
// compared structurally (numbers all become float64, key order is irrelevant).
func decodeJSON(t *testing.T, raw []byte, what string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s is not valid JSON: %v\ngot:\n%s", what, err, raw)
	}
	return v
}

// searchServer stands up a stub that always answers with searchOK.
func searchServer(t *testing.T) *cmdServer {
	t.Helper()
	return newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchOK))
	})
}

// writeFilters writes a --filters-json fixture and returns its path.
func writeFilters(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "filters.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write filters fixture: %v", err)
	}
	return path
}

// TestSearchRequestBodyFromFlags is the flag→request-body matrix. It drives the
// real command (so the FLAG NAMES are pinned too, not just the mapping) and
// asserts on the JSON the server actually received.
func TestSearchRequestBodyFromFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantBody string
	}{
		{
			name: "no filters is a legal match-everything search",
			args: nil,
			// search_params is omitted entirely rather than sent as {} — the
			// server treats an absent key as "match everything".
			wantBody: `{"page":0,"size":15}`,
		},
		{
			name:     "keywords",
			args:     []string{"--keywords", "travel,photography"},
			wantBody: `{"page":0,"size":15,"search_params":{"keywords":["travel","photography"]}}`,
		},
		{
			name:     "repeated keywords flag accumulates",
			args:     []string{"--keywords", "travel", "--keywords", "food"},
			wantBody: `{"page":0,"size":15,"search_params":{"keywords":["travel","food"]}}`,
		},
		{
			name:     "follower range, both bounds",
			args:     []string{"--followers-min", "10000", "--followers-max", "500000"},
			wantBody: `{"page":0,"size":15,"search_params":{"follower_count":{"gte":10000,"lte":500000}}}`,
		},
		{
			name: "follower range, lower bound only",
			args: []string{"--followers-min", "10000"},
			// Only the bound that was passed appears — an unset --followers-max
			// must not become `lte: 0`, which would match nothing.
			wantBody: `{"page":0,"size":15,"search_params":{"follower_count":{"gte":10000}}}`,
		},
		{
			name:     "engagement-rate range",
			args:     []string{"--er-min", "2.5", "--er-max", "10"},
			wantBody: `{"page":0,"size":15,"search_params":{"general_er":{"gte":2.5,"lte":10}}}`,
		},
		{
			name: "explicit zero lower bound is kept",
			args: []string{"--er-min", "0"},
			// The flag's zero value is also a legitimate bound, so "was it
			// passed" cannot be inferred from the value.
			wantBody: `{"page":0,"size":15,"search_params":{"general_er":{"gte":0}}}`,
		},
		{
			name: "country becomes an included location",
			args: []string{"--country", "us"},
			wantBody: `{"page":0,"size":15,"search_params":{"locations":[` +
				`{"type":"country","country":"US","should_be_included":true}]}}`,
		},
		{
			name: "multiple countries",
			args: []string{"--country", "US,DE"},
			wantBody: `{"page":0,"size":15,"search_params":{"locations":[` +
				`{"type":"country","country":"US","should_be_included":true},` +
				`{"type":"country","country":"DE","should_be_included":true}]}}`,
		},
		{
			name:     "language",
			args:     []string{"--language", "en,es"},
			wantBody: `{"page":0,"size":15,"search_params":{"languages":["en","es"]}}`,
		},
		{
			name:     "category",
			args:     []string{"--category", "Photographer"},
			wantBody: `{"page":0,"size":15,"search_params":{"instagram_category":["Photographer"]}}`,
		},
		{
			name:     "gender word becomes the numeric code the index stores",
			args:     []string{"--gender", "female"},
			wantBody: `{"page":0,"size":15,"search_params":{"gender":[2]}}`,
		},
		{
			name:     "gender is case-insensitive",
			args:     []string{"--gender", "MALE"},
			wantBody: `{"page":0,"size":15,"search_params":{"gender":[1]}}`,
		},
		{
			name:     "verified",
			args:     []string{"--verified"},
			wantBody: `{"page":0,"size":15,"search_params":{"is_verified":true}}`,
		},
		{
			name:     "verified=false is a real filter, not an absent one",
			args:     []string{"--verified=false"},
			wantBody: `{"page":0,"size":15,"search_params":{"is_verified":false}}`,
		},
		{
			name:     "with-contacts",
			args:     []string{"--with-contacts", "biography_contacts,trendhero_contacts"},
			wantBody: `{"page":0,"size":15,"search_params":{"with_contacts":["biography_contacts","trendhero_contacts"]}}`,
		},
		{
			name:     "page and size are top-level, not filters",
			args:     []string{"--page", "3", "--size", "50"},
			wantBody: `{"page":3,"size":50}`,
		},
		{
			name: "several flags combine",
			args: []string{"--keywords", "fitness", "--followers-min", "50000",
				"--country", "US", "--gender", "female", "--verified", "--size", "25"},
			wantBody: `{"page":0,"size":25,"search_params":{` +
				`"keywords":["fitness"],` +
				`"follower_count":{"gte":50000},` +
				`"locations":[{"type":"country","country":"US","should_be_included":true}],` +
				`"gender":[2],` +
				`"is_verified":true}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			cs := searchServer(t)

			args := append([]string{"search", "--base-url", cs.srv.URL, "--token", "tok"}, tt.args...)
			out, code := runCaptured(t, args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
			}

			req := cs.last()
			got := decodeJSON(t, req.body, "request body")
			want := decodeJSON(t, []byte(tt.wantBody), "expected body")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("request body mismatch\n got: %s\nwant: %s", req.body, tt.wantBody)
			}
		})
	}
}

// TestSearchFiltersJSONMergePrecedence pins the documented merge rule:
// --filters-json wins key by key at the top level of search_params, and keys it
// does not mention are left as the flags set them.
func TestSearchFiltersJSONMergePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		filters  string
		args     []string
		wantBody string
	}{
		{
			name:    "json overrides the same filter a flag set",
			filters: `{"follower_count":{"gte":1000000}}`,
			args:    []string{"--followers-min", "10000", "--followers-max", "50000"},
			// Whole-key replacement, not a blend: the flag's `lte` is gone.
			wantBody: `{"page":0,"size":15,"search_params":{"follower_count":{"gte":1000000}}}`,
		},
		{
			name:     "json adds filters the flags cannot express",
			filters:  `{"aqs":{"gte":60},"sort":[{"follower_count":"desc"}]}`,
			args:     []string{"--keywords", "travel"},
			wantBody: `{"page":0,"size":15,"search_params":{"keywords":["travel"],"aqs":{"gte":60},"sort":[{"follower_count":"desc"}]}}`,
		},
		{
			name:     "flags survive keys the json does not mention",
			filters:  `{"is_private":false}`,
			args:     []string{"--gender", "male"},
			wantBody: `{"page":0,"size":15,"search_params":{"gender":[1],"is_private":false}}`,
		},
		{
			name:     "premium audience filters pass through untouched",
			filters:  `{"audience_locations":[{"type":"country","country":"US","gte":30}],"audience_gender":{"gender":"female","gte":60}}`,
			args:     nil,
			wantBody: `{"page":0,"size":15,"search_params":{"audience_locations":[{"type":"country","country":"US","gte":30}],"audience_gender":{"gender":"female","gte":60}}}`,
		},
		{
			name:     "an empty json object leaves the flags alone",
			filters:  `{}`,
			args:     []string{"--keywords", "travel"},
			wantBody: `{"page":0,"size":15,"search_params":{"keywords":["travel"]}}`,
		},
		{
			name:     "rapidapi_age is an array of brackets, passed through verbatim",
			filters:  `{"rapidapi_age":[{"gte":18,"lte":24},{"gte":25,"lte":34}]}`,
			args:     nil,
			wantBody: `{"page":0,"size":15,"search_params":{"rapidapi_age":[{"gte":18,"lte":24},{"gte":25,"lte":34}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			cs := searchServer(t)

			args := append([]string{
				"search", "--base-url", cs.srv.URL, "--token", "tok",
				"--filters-json", writeFilters(t, tt.filters),
			}, tt.args...)
			out, code := runCaptured(t, args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
			}

			req := cs.last()
			got := decodeJSON(t, req.body, "request body")
			want := decodeJSON(t, []byte(tt.wantBody), "expected body")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("request body mismatch\n got: %s\nwant: %s", req.body, tt.wantBody)
			}
		})
	}
}

func TestSearchFiltersJSONFromStdin(t *testing.T) {
	isolateEnv(t)
	cs := searchServer(t)

	out, code := runCapturedStdin(t, `{"follower_count":{"gte":10000},"is_verified":true}`,
		"search", "--base-url", cs.srv.URL, "--token", "tok", "--filters-json", "-")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}

	req := cs.last()
	got := decodeJSON(t, req.body, "request body")
	want := decodeJSON(t,
		[]byte(`{"page":0,"size":15,"search_params":{"follower_count":{"gte":10000},"is_verified":true}}`),
		"expected body")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request body mismatch\n got: %s", req.body)
	}
}

func TestSearchSuccessPassthrough(t *testing.T) {
	isolateEnv(t)
	cs := searchServer(t)

	out, code := runCaptured(t, "search", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}

	req := cs.last()
	if req.method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.method)
	}
	if req.path != "/api/public/v1/searches" {
		t.Errorf("request path = %q, want /api/public/v1/searches", req.path)
	}
	if req.auth != "Bearer tok" {
		t.Errorf("Authorization header = %q, want %q", req.auth, "Bearer tok")
	}

	// The response is passed through unchanged — results plus pagination.
	got := decodeJSON(t, []byte(out), "command output")
	want := decodeJSON(t, []byte(searchOK), "stub response")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("output is not the API response verbatim; got:\n%s", out)
	}
}

// TestSearchZeroMatchIsSuccess pins the contract's least obvious success case:
// a search matching nothing is a 200 with an empty array, not an error. It maps
// to exit 0, so an agent must read `results`, not the exit code, to learn that
// nothing matched.
func TestSearchZeroMatchIsSuccess(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchEmpty))
	})

	out, code := runCaptured(t, "search", "--base-url", cs.srv.URL, "--token", "tok",
		"--keywords", "nothingmatchesthis")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (an empty result set is a success); out:\n%s", code, out)
	}
	if !strings.Contains(out, `"results"`) {
		t.Errorf("output missing the results envelope; got:\n%s", out)
	}
}

func TestSearchMissingToken(t *testing.T) {
	isolateEnv(t)
	// Guard server: the token check must short-circuit before any HTTP call —
	// this endpoint is billed, so a tokenless invocation must never reach it.
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	out, code := runCaptured(t, "search", "--base-url", cs.srv.URL)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (auth/missing token); out:\n%s", code, out)
	}
	if cs.count() != 0 {
		t.Errorf("server called %d time(s); want 0 (RequireToken should short-circuit)", cs.count())
	}
	if !strings.Contains(out, "TRENDHERO_TOKEN") {
		t.Errorf("error output missing TRENDHERO_TOKEN guidance; got:\n%s", out)
	}
}

// TestSearchLocalValidationMakesNoCall is the money test: every mistake the CLI
// can catch itself must fail BEFORE the request, because the request costs
// credits. Each case asserts exit 1 (usage) and zero server calls.
func TestSearchLocalValidationMakesNoCall(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{
			name:     "size below the bound",
			args:     []string{"--size", "0"},
			wantText: "between 1 and 50",
		},
		{
			name:     "size above the bound",
			args:     []string{"--size", "51"},
			wantText: "between 1 and 50",
		},
		{
			name:     "size far above the bound is rejected, not clamped",
			args:     []string{"--size", "5000"},
			wantText: "rather than clamping",
		},
		{
			name:     "negative page",
			args:     []string{"--page", "-1"},
			wantText: "0-indexed",
		},
		{
			name:     "unknown gender word",
			args:     []string{"--gender", "nonbinary"},
			wantText: "invalid --gender",
		},
		{
			name:     "unknown contact source",
			args:     []string{"--with-contacts", "carrier_pigeon"},
			wantText: "invalid --with-contacts",
		},
		{
			name:     "filters-json file does not exist",
			args:     []string{"--filters-json", "/definitely/not/a/file.json"},
			wantText: "read --filters-json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			cs := searchServer(t)

			args := append([]string{"search", "--base-url", cs.srv.URL, "--token", "tok"}, tt.args...)
			out, code := runCaptured(t, args...)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (usage); out:\n%s", code, out)
			}
			if cs.count() != 0 {
				t.Errorf("server called %d time(s); want 0 — local validation must not cost a billed call", cs.count())
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("error output missing %q; got:\n%s", tt.wantText, out)
			}
		})
	}
}

// TestSearchMalformedFiltersJSON covers the --filters-json parse failures
// separately, since each needs its own fixture content.
func TestSearchMalformedFiltersJSON(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantText string
	}{
		{
			name:     "not valid JSON",
			content:  `{"follower_count": {"gte": 1000`,
			wantText: "is not valid JSON",
		},
		{
			name:    "a JSON array is not a filter set",
			content: `[{"follower_count":{"gte":1000}}]`,
			// Mirrors the server's own InvalidFilterSetError: only an ABSENT
			// search_params means "match everything"; a non-object is a mistake.
			wantText: "must be a JSON object",
		},
		{
			name:     "a bare string is not a filter set",
			content:  `"travel"`,
			wantText: "must be a JSON object",
		},
		{
			name:     "empty file",
			content:  "  \n",
			wantText: "is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			cs := searchServer(t)

			out, code := runCaptured(t, "search", "--base-url", cs.srv.URL, "--token", "tok",
				"--filters-json", writeFilters(t, tt.content))
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (usage); out:\n%s", code, out)
			}
			if cs.count() != 0 {
				t.Errorf("server called %d time(s); want 0", cs.count())
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("error output missing %q; got:\n%s", tt.wantText, out)
			}
		})
	}
}

// TestSearchAPIErrors pins the server-side failure classes onto the exit codes
// in internal/output/errors.go, which this command relies on unchanged.
func TestSearchAPIErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantCode int
		wantText string
	}{
		{
			name:     "401 bad token",
			status:   http.StatusUnauthorized,
			body:     `{"error":"Wrong token"}`,
			wantCode: 2,
			wantText: "Wrong token",
		},
		{
			name:     "403 no AdvancedApi pack",
			status:   http.StatusForbidden,
			body:     `{"error":"You need AdvancedAPI subscription pack to use API"}`,
			wantCode: 3,
			wantText: "AdvancedAPI subscription pack",
		},
		{
			name:     "403 capability disabled by an admin",
			status:   http.StatusForbidden,
			body:     `{"error":"This API capability is disabled for your account. Please contact support."}`,
			wantCode: 3,
			wantText: "disabled for your account",
		},
		{
			name:     "422 insufficient balance",
			status:   http.StatusUnprocessableEntity,
			body:     `{"error":"You have reached your search limit. Please contact support if you need more."}`,
			wantCode: 6,
			wantText: "reached your search limit",
		},
		{
			name:     "422 page beyond the plan cap",
			status:   http.StatusUnprocessableEntity,
			body:     `{"error":"The requested page is beyond the last page available for this search."}`,
			wantCode: 6,
			wantText: "beyond the last page",
		},
		{
			name:     "422 malformed filter shape",
			status:   http.StatusUnprocessableEntity,
			body:     `{"error":"The ` + "`sort`" + ` filter must be a JSON array of objects."}`,
			wantCode: 6,
			wantText: "must be a JSON array of objects",
		},
		{
			name:     "503 unavailable",
			status:   http.StatusServiceUnavailable,
			body:     `{"error":"temporarily unavailable"}`,
			wantCode: 7,
			wantText: "temporarily unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateEnv(t)
			cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			out, code := runCaptured(t, "search", "--base-url", cs.srv.URL, "--token", "tok")
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; out:\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(out, tt.wantText) {
				t.Errorf("error output missing the surfaced API message %q; got:\n%s", tt.wantText, out)
			}
		})
	}
}

// TestSearchFlagsRegistered pins the flag NAMES the skill documentation
// promises. A rename here silently breaks every documented recipe.
func TestSearchFlagsRegistered(t *testing.T) {
	cmd := newSearchCmd()
	for _, name := range []string{
		"keywords", "followers-min", "followers-max", "er-min", "er-max",
		"country", "language", "category", "gender", "verified",
		"with-contacts", "filters-json", "page", "size",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s is not registered", name)
		}
	}
}

// TestSearchHasNoAutoPagingOrConfirmFlag pins two DELIBERATE omissions, so
// re-adding either is a conscious decision rather than a drive-by convenience:
//
//   - --all / --auto-page: paging spends per-hit credits, and a flag that
//     quietly issues N billed calls can drain a balance from one command line.
//   - --confirm: unlike `report order`, a search is cheap per call and agents
//     run many; a guard on every call trains the caller to pass it reflexively,
//     which is worse than no guard. The cost is documented instead.
func TestSearchHasNoAutoPagingOrConfirmFlag(t *testing.T) {
	cmd := newSearchCmd()
	for _, name := range []string{"all", "auto-page", "confirm"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("flag --%s must not exist on `search` — see this test's comment", name)
		}
	}
}

// searchFlagRe finds `--flag-name` mentions in prose/markdown.
var searchFlagRe = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// TestSearchDocsMatchFlags keeps `skills/th-cli/references/search.md` — the
// reference an agent reads before running the command — honest about the flag
// surface, in BOTH directions: every flag the doc names must exist, and every
// flag the command registers must be documented. A renamed flag otherwise
// breaks every documented recipe silently.
func TestSearchDocsMatchFlags(t *testing.T) {
	const docPath = "../skills/th-cli/references/search.md"

	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	doc := string(raw)

	cmd := newSearchCmd()
	root := newRootCmd()

	// Flags search.md legitimately names that are not `search` flags:
	// root-level persistent flags, flags belonging to `report` (the doc ends
	// with a search→report handoff), and the two flags the doc explains are
	// DELIBERATELY ABSENT from search.
	allowed := map[string]bool{
		"token": true, "base-url": true, "config": true, // root persistent
		"wait": true, "confirm": true, // report get / report order
		"all": true, "auto-page": true, // documented as deliberately absent
	}

	mentioned := map[string]bool{}
	for _, m := range searchFlagRe.FindAllString(doc, -1) {
		name := strings.TrimPrefix(m, "--")
		mentioned[name] = true
		if allowed[name] {
			continue
		}
		if cmd.Flags().Lookup(name) == nil && root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("%s documents --%s, which `search` does not have", docPath, name)
		}
	}

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		if !mentioned[f.Name] {
			t.Errorf("flag --%s is registered on `search` but never documented in %s", f.Name, docPath)
		}
	})
}

// TestSearchHelpDocumentsCost guards the two facts a caller most needs before
// running the command, both of which live only in the help text.
func TestSearchHelpDocumentsCost(t *testing.T) {
	out, code := runCaptured(t, "search", "--help")
	if code != 0 {
		t.Fatalf("search --help: exit code = %d, want 0", code)
	}
	for _, want := range []string{"SPENDS CREDITS", "0-INDEXED"} {
		if !strings.Contains(out, want) {
			t.Errorf("search --help does not mention %q; got:\n%s", want, out)
		}
	}
}
