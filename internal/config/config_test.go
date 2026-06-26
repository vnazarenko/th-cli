package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv removes the TRENDHERO_* variables for the duration of a test so the
// host environment cannot leak into resolution. Individual cases re-set them
// via t.Setenv as needed.
func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvToken, "")
	t.Setenv(EnvBaseURL, "")
	os.Unsetenv(EnvToken)
	os.Unsetenv(EnvBaseURL)
}

// writeConfig writes a config.yaml into a fresh temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestResolveTokenPrecedence(t *testing.T) {
	cases := []struct {
		name      string
		flagToken string
		envToken  string
		fileToken string
		want      string
	}{
		{name: "flag wins over env and file", flagToken: "flagtok", envToken: "envtok", fileToken: "filetok", want: "flagtok"},
		{name: "env wins over file when no flag", envToken: "envtok", fileToken: "filetok", want: "envtok"},
		{name: "file used when no flag or env", fileToken: "filetok", want: "filetok"},
		{name: "empty when nothing set", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			if tc.envToken != "" {
				t.Setenv(EnvToken, tc.envToken)
			}
			cfgPath := writeConfig(t, "token: "+tc.fileToken+"\n")

			got, err := Resolve(Flags{Token: tc.flagToken, Config: cfgPath})
			if err != nil {
				t.Fatalf("Resolve: unexpected error: %v", err)
			}
			if got.Token != tc.want {
				t.Errorf("token = %q, want %q", got.Token, tc.want)
			}
		})
	}
}

func TestResolveHostPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		flagURL    string
		envBaseURL string
		fileBody   string
		want       string
	}{
		{
			name:       "--base-url wins over everything",
			flagURL:    "https://flag.example/",
			envBaseURL: "https://env.example",
			fileBody:   "base_url: https://file.example\n",
			want:       "https://flag.example",
		},
		{
			name:       "TRENDHERO_BASE_URL wins over file",
			envBaseURL: "https://env.example",
			fileBody:   "base_url: https://file.example\n",
			want:       "https://env.example",
		},
		{
			name:     "config base_url used when no flag or env",
			fileBody: "base_url: https://file.example\n",
			want:     "https://file.example",
		},
		{
			name:     "defaults to the API host when nothing sets one",
			fileBody: "token: x\n",
			want:     DefaultHost,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			if tc.envBaseURL != "" {
				t.Setenv(EnvBaseURL, tc.envBaseURL)
			}
			cfgPath := writeConfig(t, tc.fileBody)

			got, err := Resolve(Flags{BaseURL: tc.flagURL, Config: cfgPath})
			if err != nil {
				t.Fatalf("Resolve: unexpected error: %v", err)
			}
			if got.BaseURL != tc.want {
				t.Errorf("base url = %q, want %q", got.BaseURL, tc.want)
			}
		})
	}
}

// With no host configured, the CLI uses the trendHERO API host so a fresh user
// only needs a token.
func TestDefaultHostIsProd(t *testing.T) {
	clearEnv(t)
	cfgPath := writeConfig(t, "")
	got, err := Resolve(Flags{Config: cfgPath})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if got.BaseURL != DefaultHost {
		t.Errorf("default base url = %q, want %q", got.BaseURL, DefaultHost)
	}
}

// TestInvalidBaseURLRejected covers up-front rejection of base URLs the API
// client cannot use: a missing scheme, a full API URL (which would double the
// /api/public prefix), and a host-less value. Each must fail at resolution as an
// *InvalidBaseURLError (exit 1) rather than passing through to a later network
// error.
func TestInvalidBaseURLRejected(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
	}{
		{name: "missing scheme", baseURL: "host.example"},
		{name: "full api url doubles prefix", baseURL: "https://host.example/api/public/v1"},
		{name: "api prefix without version", baseURL: "https://host/api/public"},
		{name: "scheme only no host", baseURL: "https://"},
		{name: "unsupported scheme", baseURL: "ftp://host"},
		// A query/fragment is not "host only": appending /api/public by raw string
		// concatenation would push the prefix into the query/fragment and drop it
		// during URL resolution, silently hitting the wrong path.
		{name: "query string", baseURL: "https://host?x=1"},
		{name: "empty query (force)", baseURL: "https://host?"},
		{name: "fragment", baseURL: "https://host#frag"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			cfgPath := writeConfig(t, "")
			_, err := Resolve(Flags{BaseURL: tc.baseURL, Config: cfgPath})
			var badURL *InvalidBaseURLError
			if !errors.As(err, &badURL) {
				t.Fatalf("Resolve(--base-url %q) error = %v, want *InvalidBaseURLError", tc.baseURL, err)
			}
		})
	}
}

// TestValidBaseURLAccepted guards against the validation being too strict: a
// bare host (with or without port) and an unrelated path prefix must resolve
// cleanly.
func TestValidBaseURLAccepted(t *testing.T) {
	for _, in := range []string{
		"https://trendhero.io",
		"http://host.example:4000",
		"https://host/custom-prefix",
	} {
		clearEnv(t)
		cfgPath := writeConfig(t, "")
		if _, err := Resolve(Flags{BaseURL: in, Config: cfgPath}); err != nil {
			t.Errorf("Resolve(--base-url %q): unexpected error: %v", in, err)
		}
	}
}

// TestWritesAllowed exercises the truthy/falsy parsing of the standing paid-write
// opt-in. This gates a PAID action, so the recognized values are pinned here.
func TestWritesAllowed(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"Yes", true},
		{"  true  ", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"nope", false},
		{"2", false},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv(EnvAllowWrites, tc.val)
			if got := WritesAllowed(); got != tc.want {
				t.Errorf("WritesAllowed() with %s=%q = %v, want %v", EnvAllowWrites, tc.val, got, tc.want)
			}
		})
	}
}

func TestHostTrailingSlashNormalization(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "single trailing slash", in: "https://x.example/", want: "https://x.example"},
		{name: "multiple trailing slashes", in: "https://x.example///", want: "https://x.example"},
		{name: "no trailing slash unchanged", in: "https://x.example", want: "https://x.example"},
		{name: "surrounding whitespace trimmed", in: "  https://x.example/  ", want: "https://x.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			cfgPath := writeConfig(t, "")
			// Exercise normalization via --base-url...
			got, err := Resolve(Flags{BaseURL: tc.in, Config: cfgPath})
			if err != nil {
				t.Fatalf("Resolve: unexpected error: %v", err)
			}
			if got.BaseURL != tc.want {
				t.Errorf("--base-url %q -> %q, want %q", tc.in, got.BaseURL, tc.want)
			}
			// ...and via TRENDHERO_BASE_URL, which must normalize identically.
			t.Setenv(EnvBaseURL, tc.in)
			got, err = Resolve(Flags{Config: cfgPath})
			if err != nil {
				t.Fatalf("Resolve (env): unexpected error: %v", err)
			}
			if got.BaseURL != tc.want {
				t.Errorf("%s=%q -> %q, want %q", EnvBaseURL, tc.in, got.BaseURL, tc.want)
			}
		})
	}
}

func TestRequireTokenMissingMessage(t *testing.T) {
	err := Config{Token: ""}.RequireToken()
	var missing *MissingTokenError
	if !errors.As(err, &missing) {
		t.Fatalf("RequireToken() error = %v, want *MissingTokenError", err)
	}
	msg := err.Error()
	for _, want := range []string{EnvToken, "/app/api/access-tokens", "AdvancedApi"} {
		if !strings.Contains(msg, want) {
			t.Errorf("MissingTokenError message %q must mention %q", msg, want)
		}
	}
}

func TestRequireTokenPresentNoError(t *testing.T) {
	if err := (Config{Token: "tok"}).RequireToken(); err != nil {
		t.Errorf("RequireToken() with token set = %v, want nil", err)
	}
}

// A missing token is fine for resolution itself — only report commands (via
// RequireToken) treat it as an error. top-profiles relies on this.
func TestResolveMissingTokenIsNotAnError(t *testing.T) {
	clearEnv(t)
	cfgPath := writeConfig(t, "base_url: https://x.example\n")
	cfg, err := Resolve(Flags{Config: cfgPath})
	if err != nil {
		t.Fatalf("Resolve with no token: unexpected error: %v", err)
	}
	if cfg.Token != "" {
		t.Errorf("token = %q, want empty", cfg.Token)
	}
}

func TestDefaultConfigFileIsOptional(t *testing.T) {
	clearEnv(t)
	// Point HOME at an empty dir: the default config file does not exist, which
	// must not be an error.
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := Resolve(Flags{})
	if err != nil {
		t.Fatalf("Resolve with no default config file: unexpected error: %v", err)
	}
	if cfg.Token != "" {
		t.Errorf("token = %q, want empty", cfg.Token)
	}
	if cfg.BaseURL != DefaultHost {
		t.Errorf("base url = %q, want default %q", cfg.BaseURL, DefaultHost)
	}
}

func TestDefaultConfigFileIsRead(t *testing.T) {
	clearEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".config", "th-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "token: deftok\nbase_url: https://default.example/\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Resolve(Flags{})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if cfg.Token != "deftok" {
		t.Errorf("token = %q, want deftok", cfg.Token)
	}
	if cfg.BaseURL != "https://default.example" {
		t.Errorf("base url = %q, want normalized https://default.example", cfg.BaseURL)
	}
}

func TestExplicitMissingConfigIsError(t *testing.T) {
	clearEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if _, err := Resolve(Flags{Config: missing}); err == nil {
		t.Fatalf("Resolve with explicit missing --config: want error, got nil")
	}
}

func TestUnparseableConfigIsError(t *testing.T) {
	clearEnv(t)
	cfgPath := writeConfig(t, "token: [unterminated\n")
	if _, err := Resolve(Flags{Config: cfgPath}); err == nil {
		t.Fatalf("Resolve with malformed config: want error, got nil")
	}
}
