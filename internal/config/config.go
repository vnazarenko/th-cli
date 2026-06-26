// Package config resolves the effective `th-cli` configuration from three layered
// sources, highest precedence first:
//
//  1. command-line flags (--token, --base-url, --config)
//  2. environment variables (TRENDHERO_TOKEN, TRENDHERO_BASE_URL)
//  3. the YAML config file at ~/.config/th-cli/config.yaml (keys: token, base_url)
//
// Token precedence is flag > env > file; host precedence is the same. With no
// host configured the CLI uses the trendHERO API, so a fresh user only needs
// a token. A token is never required to *resolve* a config (top-profiles works
// without one); report commands enforce its presence via Config.RequireToken.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vnazarenko/th-cli/internal/output"
)

// DefaultHost is the trendHERO API host, used when no host is
// configured.
const DefaultHost = "https://trendhero.io"

// Environment variable names read during resolution.
const (
	EnvToken   = "TRENDHERO_TOKEN"
	EnvBaseURL = "TRENDHERO_BASE_URL"
	// EnvAllowWrites globally opts into paid write actions (report order),
	// the per-invocation equivalent of the --confirm flag. Recognized truthy
	// values are 1/true/yes (case-insensitive).
	EnvAllowWrites = "TRENDHERO_ALLOW_WRITES"
)

// defaultConfigRelPath is the config file location relative to the user's home
// directory. The plan pins this to ~/.config/th-cli/config.yaml on all platforms,
// so we build it from the home dir rather than os.UserConfigDir (which differs
// on macOS/Windows).
var defaultConfigRelPath = filepath.Join(".config", "th-cli", "config.yaml")

// Flags carries the raw values of the root command's persistent flags. The cmd
// package populates it; Resolve merges it with env and file sources.
type Flags struct {
	Token   string // --token
	BaseURL string // --base-url
	Config  string // --config (explicit config file path; empty = default)
}

// Config is the resolved configuration the rest of the CLI consumes. BaseURL is
// the normalized API host (no trailing slash); the API client appends the
// /api/public prefix itself.
type Config struct {
	Token   string
	BaseURL string
}

// fileConfig mirrors the on-disk YAML config file.
type fileConfig struct {
	Token   string `yaml:"token"`
	BaseURL string `yaml:"base_url"`
}

// MissingTokenError is returned by RequireToken when no AccessToken is
// configured. Its message points the user at the exact knobs and prerequisites.
type MissingTokenError struct{}

func (*MissingTokenError) Error() string {
	return "no trendHERO AccessToken configured. " +
		"Get your token at https://trendhero.io/app/api/access-tokens " +
		"(requires the AdvancedApi subscription), then set it one of these ways:\n" +
		"  • export " + EnvToken + "=<token>\n" +
		"  • pass --token <token>\n" +
		"  • add `token: <token>` to ~/.config/th-cli/config.yaml"
}

// ExitCode classes a missing token as an auth failure (exit 2) so report
// commands surface it the same way the API's own 401 would, with the matching
// "set TRENDHERO_TOKEN" hint. It implements output.ExitCoder.
func (*MissingTokenError) ExitCode() int { return output.ExitAuth }

// InvalidBaseURLError is returned when a configured base URL is not a usable API
// host. base_url is meant to be a bare host (scheme + host[:port]); the API
// client appends the /api/public prefix itself. Reason explains what is wrong.
// It carries no ExitCode method, so it maps to the generic usage class (exit 1)
// — a config mistake, surfaced before any network attempt rather than as a later
// misleading 404/timeout.
type InvalidBaseURLError struct {
	Host   string
	Reason string
}

func (e *InvalidBaseURLError) Error() string {
	return fmt.Sprintf("invalid base URL %q: %s", e.Host, e.Reason)
}

// RequireToken returns a *MissingTokenError when no token is set. Report
// commands call this; top-profiles never does (the token is optional there).
func (c Config) RequireToken() error {
	if c.Token == "" {
		return &MissingTokenError{}
	}
	return nil
}

// WritesAllowed reports whether paid write actions are globally enabled via the
// TRENDHERO_ALLOW_WRITES env var. Recognized truthy values are 1/true/yes
// (case-insensitive). It is the standing equivalent of a command's --confirm
// flag; `report order` proceeds if either is set.
func WritesAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAllowWrites))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// Resolve merges flags, environment variables, and the config file into a
// Config. It returns an error only for a genuinely broken setup: an
// unreadable/unparseable config file or an invalid base URL. A missing token is
// never an error here.
func Resolve(flags Flags) (Config, error) {
	fileCfg, err := loadFileConfig(flags.Config)
	if err != nil {
		return Config{}, err
	}

	host, err := resolveHost(flags, fileCfg)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Token:   resolveToken(flags, fileCfg),
		BaseURL: host,
	}, nil
}

// resolveToken applies token precedence: flag > env > file.
func resolveToken(flags Flags, fileCfg fileConfig) string {
	if flags.Token != "" {
		return flags.Token
	}
	if v := os.Getenv(EnvToken); v != "" {
		return v
	}
	return fileCfg.Token
}

// resolveHost applies host precedence:
// --base-url > TRENDHERO_BASE_URL > config base_url, defaulting to the trendHERO API host
// (DefaultHost) so a fresh user with just a token works out of the box. The
// resolved host is normalized (trimmed, no trailing slash) and validated.
func resolveHost(flags Flags, fileCfg fileConfig) (string, error) {
	raw := rawHost(flags, fileCfg)
	if raw == "" {
		// Nothing configured — use the default host.
		raw = DefaultHost
	}
	host := normalizeHost(raw)
	if err := validateHost(host); err != nil {
		return "", err
	}
	return host, nil
}

// rawHost returns the first host configured by precedence, unnormalized, or ""
// when none is set (the caller then uses the default host).
func rawHost(flags Flags, fileCfg fileConfig) string {
	if flags.BaseURL != "" {
		return flags.BaseURL
	}
	if v := os.Getenv(EnvBaseURL); v != "" {
		return v
	}
	return fileCfg.BaseURL
}

// validateHost rejects a base URL the API client cannot turn into working
// requests. base_url is a bare host (scheme + host[:port]); the client appends
// /api/public itself. We require an http/https scheme and a host so a
// missing-scheme or otherwise malformed value fails here as a clear config error
// (exit 1) instead of surfacing later as a confusing network error. A query
// string or fragment is rejected too: the client forms its server by raw string
// concatenation ({host}+"/api/public"), so a value like "https://host?x=1" would
// turn into "https://host?x=1/api/public" — the prefix lands inside the query and
// is dropped during URL resolution, silently sending requests to the wrong path.
// A path that already carries the API prefix is the common "pasted the full API
// URL" mistake, which would otherwise double the prefix (…/api/public/v1/api/public/…)
// and 404; we reject it with a pointed hint. The "/api/public" literal mirrors
// api.apiPrefix, duplicated here because config cannot import api (api imports
// config).
func validateHost(host string) error {
	u, err := url.Parse(host)
	if err != nil {
		return &InvalidBaseURLError{Host: host, Reason: err.Error()}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &InvalidBaseURLError{Host: host, Reason: "must start with http:// or https://"}
	}
	if u.Host == "" {
		return &InvalidBaseURLError{Host: host, Reason: "missing host"}
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return &InvalidBaseURLError{Host: host, Reason: "must be a bare host with no query string or " +
			"fragment (e.g. https://trendhero.io) — the /api/public prefix is added automatically"}
	}
	if strings.Contains(u.Path, "/api/public/") || strings.HasSuffix(u.Path, "/api/public") {
		return &InvalidBaseURLError{Host: host, Reason: "looks like a full API URL; use just " +
			"the host (e.g. https://trendhero.io) — the /api/public prefix is added automatically"}
	}
	return nil
}

// normalizeHost trims surrounding whitespace and any trailing slashes so the
// API client can append /api/public without producing a double slash.
func normalizeHost(host string) string {
	return strings.TrimRight(strings.TrimSpace(host), "/")
}

// loadFileConfig reads and parses the YAML config file. An explicit --config
// path that does not exist is an error; a missing *default* file is not (the
// config file is optional). A present-but-unparseable file is always an error.
func loadFileConfig(explicitPath string) (fileConfig, error) {
	path := explicitPath
	explicit := path != ""
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Cannot locate the home dir; treat as "no config file present".
			return fileConfig{}, nil
		}
		path = filepath.Join(home, defaultConfigRelPath)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return fileConfig{}, nil
		}
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var fc fileConfig
	if err := yaml.Unmarshal(raw, &fc); err != nil {
		return fileConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return fc, nil
}
