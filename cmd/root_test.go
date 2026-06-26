package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vnazarenko/th-cli/internal/output"
)

// runCaptured runs the root command with the given args while capturing its
// stdout/stderr, returning the combined output and the exit code. It mirrors
// run() — including the JSON error envelope and output.ExitCode mapping — but
// captures the streams into one buffer so tests can assert on output without
// touching the real ones.
func runCaptured(t *testing.T, args ...string) (string, int) {
	t.Helper()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	code := 0
	if err := root.Execute(); err != nil {
		_ = output.WriteError(&out, err)
		code = output.ExitCode(err)
	}
	return out.String(), code
}

func TestHelpExitsZero(t *testing.T) {
	out, code := runCaptured(t, "--help")
	if code != 0 {
		t.Fatalf("th-cli --help: exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "th-cli") {
		t.Errorf("th-cli --help: output missing command name; got:\n%s", out)
	}
}

func TestUnknownCommandExitsOne(t *testing.T) {
	_, code := runCaptured(t, "definitely-not-a-command")
	if code != 1 {
		t.Fatalf("unknown command: exit code = %d, want 1", code)
	}
}

func TestUnknownFlagExitsOne(t *testing.T) {
	_, code := runCaptured(t, "--definitely-not-a-flag")
	if code != 1 {
		t.Fatalf("unknown flag: exit code = %d, want 1", code)
	}
}

func TestPersistentFlagsRegistered(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{"token", "base-url", "config"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("persistent flag --%s is not registered", name)
		}
	}
}

func TestRunHelpViaSeam(t *testing.T) {
	// run() is the production seam Execute() delegates to; help must exit 0.
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("run(--help): exit code = %d, want 0", code)
	}
}
