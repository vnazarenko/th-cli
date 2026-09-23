package api

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// specFile is the authored OpenAPI contract under test. It is read relative to
// the package directory (Go tests run with the package dir as the working dir).
const specFile = "public-api.openapi.yaml"

// operation is a minimal view of an OpenAPI operation object — enough to assert
// the operationId and the (per-operation) security requirement.
type operation struct {
	OperationID string              `yaml:"operationId"`
	Summary     string              `yaml:"summary"`
	Security    *[]map[string][]any `yaml:"security"`
}

// pathItem captures only the HTTP methods this spec actually uses.
type pathItem struct {
	Get  *operation `yaml:"get"`
	Post *operation `yaml:"post"`
}

type securityScheme struct {
	Type   string `yaml:"type"`
	Scheme string `yaml:"scheme"`
}

type openAPISpec struct {
	OpenAPI string `yaml:"openapi"`
	Servers []struct {
		URL string `yaml:"url"`
	} `yaml:"servers"`
	Paths      map[string]pathItem `yaml:"paths"`
	Components struct {
		SecuritySchemes map[string]securityScheme `yaml:"securitySchemes"`
		Schemas         map[string]struct {
			Type       string                    `yaml:"type"`
			Properties map[string]map[string]any `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

// loadSpec reads and unmarshals the spec, failing the test if it does not parse.
func loadSpec(t *testing.T) openAPISpec {
	t.Helper()
	raw, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec openAPISpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("spec does not parse as YAML: %v", err)
	}
	return spec
}

func TestSpecParses(t *testing.T) {
	spec := loadSpec(t)
	if !strings.HasPrefix(spec.OpenAPI, "3.0") {
		t.Errorf("openapi version = %q, want 3.0.x", spec.OpenAPI)
	}
}

func TestServerSharesSinglePublicPrefix(t *testing.T) {
	spec := loadSpec(t)
	if len(spec.Servers) != 1 {
		t.Fatalf("servers = %d, want exactly 1 (single shared base)", len(spec.Servers))
	}
	url := spec.Servers[0].URL
	// The host is templated and /api/public is appended once so top_profiles
	// and reports share one clean prefix with no double /api.
	if !strings.Contains(url, "{host}") {
		t.Errorf("server url %q missing templated {host}", url)
	}
	if !strings.HasSuffix(url, "/api/public") {
		t.Errorf("server url %q must end in /api/public", url)
	}
}

// TestExpectedOperations pins the operationIds the generated client (and the
// cmd layer that calls it) depends on. Renaming an operationId renames a
// generated method, so this is a compile-break early-warning as much as a spec
// check.
//
// Was TestThreeExpectedOperations until POST /v1/searches made it four; the
// count is deliberately no longer in the name so the next operation does not
// have to rename it again.
func TestExpectedOperations(t *testing.T) {
	spec := loadSpec(t)

	tests := []struct {
		path   string
		method string // "get" or "post"
		opID   string
	}{
		{"/top_profiles", "get", "getTopProfiles"},
		{"/v1/reports/{username}", "get", "getReport"},
		{"/v1/reports", "post", "createReport"},
		{"/v1/searches", "post", "searchAccounts"},
	}

	for _, tt := range tests {
		item, ok := spec.Paths[tt.path]
		if !ok {
			t.Errorf("missing path %s", tt.path)
			continue
		}
		op := item.Get
		if tt.method == "post" {
			op = item.Post
		}
		if op == nil {
			t.Errorf("missing %s %s", strings.ToUpper(tt.method), tt.path)
			continue
		}
		if op.OperationID != tt.opID {
			t.Errorf("%s %s operationId = %q, want %s",
				strings.ToUpper(tt.method), tt.path, op.OperationID, tt.opID)
		}
	}

	// Guard against an operation being added to the spec without being added
	// here — the list above is only a contract if it is exhaustive.
	declared := 0
	for _, item := range spec.Paths {
		if item.Get != nil {
			declared++
		}
		if item.Post != nil {
			declared++
		}
	}
	if declared != len(tests) {
		t.Errorf("spec declares %d operations but this test pins %d; add the new one here", declared, len(tests))
	}
}

func TestBearerSchemeDeclared(t *testing.T) {
	spec := loadSpec(t)
	bearer, ok := spec.Components.SecuritySchemes["bearerAuth"]
	if !ok {
		t.Fatalf("components.securitySchemes.bearerAuth not declared")
	}
	if bearer.Type != "http" || bearer.Scheme != "bearer" {
		t.Errorf("bearerAuth = {type:%q scheme:%q}, want {http bearer}", bearer.Type, bearer.Scheme)
	}
}

// hasBearer reports whether an operation's security requirement lists bearerAuth.
func hasBearer(op *operation) bool {
	if op == nil || op.Security == nil {
		return false
	}
	for _, req := range *op.Security {
		if _, ok := req["bearerAuth"]; ok {
			return true
		}
	}
	return false
}

func TestReportsAreBearerGuarded(t *testing.T) {
	spec := loadSpec(t)

	if !hasBearer(spec.Paths["/v1/reports/{username}"].Get) {
		t.Errorf("GET /v1/reports/{username} must require bearerAuth")
	}
	if !hasBearer(spec.Paths["/v1/reports"].Post) {
		t.Errorf("POST /v1/reports must require bearerAuth")
	}
}

// TestSearchIsBearerGuarded is separate from the reports guard because search
// is a distinct, PAID surface: an accidentally unauthenticated search endpoint
// would be an open door onto a metered resource, not merely a data leak.
func TestSearchIsBearerGuarded(t *testing.T) {
	spec := loadSpec(t)
	if !hasBearer(spec.Paths["/v1/searches"].Post) {
		t.Errorf("POST /v1/searches must require bearerAuth")
	}
}

// TestSearchSchemasModelled pins the parts of the search contract the CLI and
// its docs depend on: the filter surface is complete (all 30 permitted keys),
// the paging envelope is modelled, and the two easily-mistaken container shapes
// (`rapidapi_age` and `sort` are ARRAYS) are documented as arrays.
func TestSearchSchemasModelled(t *testing.T) {
	spec := loadSpec(t)

	params, ok := spec.Components.Schemas["SearchParams"]
	if !ok {
		t.Fatalf("components.schemas.SearchParams missing")
	}

	// Mirrors Search::Users::ParamsValidator::SHAPES_BY_FILTER in web-api,
	// which is derived from the query builder and pinned there against the
	// controller's PERMITTED_SEARCH_PARAMS. 30 keys.
	permitted := []string{
		// object (12 range filters + audience_gender)
		"follower_count", "following_count", "media_count", "last_post_at",
		"median_likes", "median_comments", "general_er", "aqs",
		"audience_authentic", "follower_growth_7", "follower_growth_30",
		"follower_growth_90", "audience_gender",
		// object arrays
		"sort", "rapidapi_age", "audience_locations",
		// location array
		"locations",
		// strings
		"required_keywords", "exclude_keywords",
		// string arrays
		"keywords", "languages", "instagram_category", "mega_categories", "with_contacts",
		// scalar / scalar array
		"account_type", "pks",
		// booleans
		"is_verified", "is_private",
		// gender arrays
		"gender", "brand",
	}
	if len(permitted) != 30 {
		t.Fatalf("the permitted-filter list in this test has %d entries, want 30", len(permitted))
	}
	for _, key := range permitted {
		if _, ok := params.Properties[key]; !ok {
			t.Errorf("SearchParams missing permitted filter %q", key)
		}
	}
	if got := len(params.Properties); got != len(permitted) {
		t.Errorf("SearchParams models %d filters, want exactly the %d the server permits",
			got, len(permitted))
	}

	// audience_male is the server's INTERNAL normalisation of audience_gender
	// and is deliberately not permitted; modelling it would advertise a filter
	// the API drops.
	if _, ok := params.Properties["audience_male"]; ok {
		t.Errorf("SearchParams must not model audience_male — the API does not permit it")
	}

	// The two shapes callers get wrong most often. Both are arrays, and a bare
	// object is a 422, so the spec must not describe them as objects.
	for _, key := range []string{"sort", "rapidapi_age", "audience_locations", "locations"} {
		if got := params.Properties[key]["type"]; got != "array" {
			t.Errorf("SearchParams.%s type = %v, want array", key, got)
		}
	}

	// The paging envelope the CLI's docs describe.
	pagination, ok := spec.Components.Schemas["Pagination"]
	if !ok {
		t.Fatalf("components.schemas.Pagination missing")
	}
	for _, field := range []string{"page", "size", "total_pages", "total_results"} {
		if _, ok := pagination.Properties[field]; !ok {
			t.Errorf("Pagination missing field %q", field)
		}
	}

	resp, ok := spec.Components.Schemas["SearchResponse"]
	if !ok {
		t.Fatalf("components.schemas.SearchResponse missing")
	}
	for _, field := range []string{"results", "pagination"} {
		if _, ok := resp.Properties[field]; !ok {
			t.Errorf("SearchResponse missing field %q", field)
		}
	}
}

// TestSearchSizeBoundDocumented pins both halves of the `size` contract: the
// 1..50 schema bound (client-side courtesy) AND the prose saying the server
// answers 422 rather than clamping. Losing the prose would leave a client
// author reasonably assuming a clamp.
func TestSearchSizeBoundDocumented(t *testing.T) {
	raw, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	spec := loadSpec(t)

	req, ok := spec.Components.Schemas["SearchRequest"]
	if !ok {
		t.Fatalf("components.schemas.SearchRequest missing")
	}
	size, ok := req.Properties["size"]
	if !ok {
		t.Fatalf("SearchRequest.size missing")
	}
	if size["minimum"] != 1 {
		t.Errorf("SearchRequest.size minimum = %v, want 1", size["minimum"])
	}
	if size["maximum"] != 50 {
		t.Errorf("SearchRequest.size maximum = %v, want 50", size["maximum"])
	}

	page, ok := req.Properties["page"]
	if !ok {
		t.Fatalf("SearchRequest.page missing")
	}
	if page["minimum"] != 0 {
		t.Errorf("SearchRequest.page minimum = %v, want 0 (pages are 0-indexed)", page["minimum"])
	}

	// The two facts a generated client cannot express in schema keywords.
	for _, phrase := range []string{"never clamp", "0-indexed"} {
		if !strings.Contains(strings.ToLower(string(raw)), strings.ToLower(phrase)) {
			t.Errorf("spec does not document %q anywhere", phrase)
		}
	}
}

func TestTopProfilesIsUnauthenticated(t *testing.T) {
	spec := loadSpec(t)
	top := spec.Paths["/top_profiles"].Get
	if hasBearer(top) {
		t.Errorf("GET /top_profiles must NOT require bearerAuth (it is unauthenticated)")
	}
	// It is declared with an explicit empty security ([]), distinguishing
	// "intentionally public" from "inherits a default" — assert that intent.
	if top.Security == nil {
		t.Errorf("GET /top_profiles should declare an explicit empty `security: []`")
	} else if len(*top.Security) != 0 {
		t.Errorf("GET /top_profiles security = %v, want empty (unauthenticated)", *top.Security)
	}
}

func TestTopProfileSchemaTyped(t *testing.T) {
	spec := loadSpec(t)
	tp, ok := spec.Components.Schemas["TopProfile"]
	if !ok {
		t.Fatalf("components.schemas.TopProfile missing — top_profiles items must be typed")
	}
	// Spot-check the fields the CLI actually consumes are modelled.
	for _, field := range []string{"username", "pk", "country", "follower_count", "general_er", "profile_pic_url"} {
		if _, ok := tp.Properties[field]; !ok {
			t.Errorf("TopProfile missing modelled field %q", field)
		}
	}
}
