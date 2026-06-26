package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vnazarenko/th-cli/internal/api"
)

func TestVersionExitsZero(t *testing.T) {
	out, code := runCaptured(t, "version")
	if code != 0 {
		t.Fatalf("th-cli version: exit code = %d, want 0", code)
	}
	// The default build metadata is "dev" (overridden by -ldflags at release).
	if !strings.Contains(out, api.Version) {
		t.Errorf("th-cli version: output missing version %q; got:\n%s", api.Version, out)
	}
}

func TestVersionEmitsJSON(t *testing.T) {
	out, code := runCaptured(t, "version")
	if code != 0 {
		t.Fatalf("th-cli version: exit code = %d, want 0", code)
	}

	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("th-cli version: output is not valid JSON: %v\ngot:\n%s", err, out)
	}
	if got.Version != api.Version {
		t.Errorf("version field = %q, want %q", got.Version, api.Version)
	}
	if got.Commit != api.Commit {
		t.Errorf("commit field = %q, want %q", got.Commit, api.Commit)
	}
}

func TestVersionRejectsArgs(t *testing.T) {
	// `version` takes no positional args; a stray one is a usage error (exit 1).
	_, code := runCaptured(t, "version", "extra")
	if code != 1 {
		t.Fatalf("th-cli version extra: exit code = %d, want 1", code)
	}
}
