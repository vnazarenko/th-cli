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

func TestThreeExpectedOperations(t *testing.T) {
	spec := loadSpec(t)

	top, ok := spec.Paths["/top_profiles"]
	if !ok || top.Get == nil {
		t.Fatalf("missing GET /top_profiles")
	}
	if top.Get.OperationID != "getTopProfiles" {
		t.Errorf("GET /top_profiles operationId = %q, want getTopProfiles", top.Get.OperationID)
	}

	getRep, ok := spec.Paths["/v1/reports/{username}"]
	if !ok || getRep.Get == nil {
		t.Fatalf("missing GET /v1/reports/{username}")
	}
	if getRep.Get.OperationID != "getReport" {
		t.Errorf("GET /v1/reports/{username} operationId = %q, want getReport", getRep.Get.OperationID)
	}

	postRep, ok := spec.Paths["/v1/reports"]
	if !ok || postRep.Post == nil {
		t.Fatalf("missing POST /v1/reports")
	}
	if postRep.Post.OperationID != "createReport" {
		t.Errorf("POST /v1/reports operationId = %q, want createReport", postRep.Post.OperationID)
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
