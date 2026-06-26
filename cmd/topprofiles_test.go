package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/vnazarenko/th-cli/internal/config"
)

// isolateEnv removes the TRENDHERO_* variables and repoints HOME at a temp dir
// so neither the host environment nor a real ~/.config/th-cli/config.yaml can leak
// into config resolution during a command test.
func isolateEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvToken, "")
	t.Setenv(config.EnvBaseURL, "")
	t.Setenv(config.EnvAllowWrites, "")
	os.Unsetenv(config.EnvToken)
	os.Unsetenv(config.EnvBaseURL)
	os.Unsetenv(config.EnvAllowWrites)
	t.Setenv("HOME", t.TempDir())
}

// cmdServer is an httptest server that records each inbound request so command
// tests can assert on the path and Authorization header the CLI produced.
type cmdServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []cmdRequest
}

type cmdRequest struct {
	method string
	path   string
	auth   string
	query  url.Values
}

func newCmdServer(t *testing.T, h http.HandlerFunc) *cmdServer {
	t.Helper()
	cs := &cmdServer{}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		cs.reqs = append(cs.reqs, cmdRequest{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			query:  r.URL.Query(),
		})
		cs.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

func (cs *cmdServer) count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.reqs)
}

func (cs *cmdServer) last() cmdRequest {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.reqs[len(cs.reqs)-1]
}

// topProfilesFixture is a minimal but representative top_profiles response.
const topProfilesFixture = `[
  {"username":"cristiano","full_name":"Cristiano Ronaldo","pk":"173560420","country":"US","follower_count":600000000,"is_verified":true},
  {"username":"leomessi","full_name":"Leo Messi","pk":"427553890","country":"US","follower_count":500000000,"is_verified":true}
]`

func TestTopProfilesSuccessNoToken(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(topProfilesFixture))
	})

	out, code := runCaptured(t, "top-profiles", "--base-url", cs.srv.URL, "--country", "US")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}

	// Hits the unauthenticated top_profiles path under the single /api/public prefix.
	req := cs.last()
	if req.path != "/api/public/top_profiles" {
		t.Errorf("request path = %q, want /api/public/top_profiles", req.path)
	}
	// No token configured → no Authorization header sent.
	if req.auth != "" {
		t.Errorf("Authorization header = %q, want empty (no token configured)", req.auth)
	}
	if got := req.query.Get("country"); got != "US" {
		t.Errorf("country query = %q, want US", got)
	}
	if got := req.query.Get("type"); got != "absolute" {
		t.Errorf("type query = %q, want absolute (default)", got)
	}
	// Unset year/month are omitted so the server applies its own defaults; the
	// sentinel path must survive the explicit-provided (Changed) refactor.
	if _, ok := req.query["year"]; ok {
		t.Errorf("year query present (%v); want omitted when --year is unset", req.query["year"])
	}
	if _, ok := req.query["month"]; ok {
		t.Errorf("month query present (%v); want omitted when --month is unset", req.query["month"])
	}

	// Output is the JSON list, passed straight through.
	var profiles []map[string]any
	if err := json.Unmarshal([]byte(out), &profiles); err != nil {
		t.Fatalf("output is not a JSON array: %v\nout:\n%s", err, out)
	}
	if len(profiles) != 2 || profiles[0]["username"] != "cristiano" {
		t.Errorf("unexpected profiles payload: %v", profiles)
	}
}

func TestTopProfilesSendsTokenWhenPresent(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(topProfilesFixture))
	})

	out, code := runCaptured(t, "top-profiles", "--base-url", cs.srv.URL, "--token", "secret-tok")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}

	if got := cs.last().auth; got != "Bearer secret-tok" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer secret-tok")
	}
}

func TestTopProfilesInvalidCountry(t *testing.T) {
	isolateEnv(t)
	// Point at a server that fails the test if it is ever hit — validation must
	// reject the bad country before any network call.
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	out, code := runCaptured(t, "top-profiles", "--base-url", cs.srv.URL, "--country", "ZZ")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (usage error); out:\n%s", code, out)
	}
	if cs.count() != 0 {
		t.Errorf("server was called %d time(s); want 0 (validation should short-circuit)", cs.count())
	}
	if !strings.Contains(out, "invalid --country") {
		t.Errorf("error output missing validation message; got:\n%s", out)
	}
}

func TestTopProfilesInvalidMonth(t *testing.T) {
	isolateEnv(t)
	// An out-of-range month must be rejected locally (exit 1) before any network
	// call — mirroring the --country/--type validation.
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// An explicit --month=0 is rejected too: 0 is out of the 1-12 range, and
	// without the explicit-provided check it would be indistinguishable from unset.
	for _, month := range []string{"--month=13", "--month=-1", "--month=0"} {
		out, code := runCaptured(t, "top-profiles", "--base-url", cs.srv.URL, month)
		if code != 1 {
			t.Fatalf("%s: exit code = %d, want 1 (usage error); out:\n%s", month, code, out)
		}
		if !strings.Contains(out, "invalid --month") {
			t.Errorf("%s: error output missing validation message; got:\n%s", month, out)
		}
	}
	if cs.count() != 0 {
		t.Errorf("server was called %d time(s); want 0 (validation should short-circuit)", cs.count())
	}
}

func TestTopProfilesInvalidYear(t *testing.T) {
	isolateEnv(t)
	// A non-positive year must be rejected locally (exit 1) rather than silently
	// dropped and queried against the server's default year — mirroring --month.
	// An explicit --year=0 is rejected too: 0 is only the "unset" sentinel when
	// the flag is omitted, never when the user passes it.
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, year := range []string{"--year=-1", "--year=0"} {
		out, code := runCaptured(t, "top-profiles", "--base-url", cs.srv.URL, year)
		if code != 1 {
			t.Fatalf("%s: exit code = %d, want 1 (usage error); out:\n%s", year, code, out)
		}
		if !strings.Contains(out, "invalid --year") {
			t.Errorf("%s: error output missing validation message; got:\n%s", year, out)
		}
	}
	if cs.count() != 0 {
		t.Errorf("server was called %d time(s); want 0 (validation should short-circuit)", cs.count())
	}
}

func TestTopProfilesAPI422(t *testing.T) {
	isolateEnv(t)
	// A 422 validation error from the API carries an error.message.
	const body = `{"error":{"message":"Growth ranking is in progress"}}`
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(body))
	})

	out, code := runCaptured(t, "top-profiles", "--base-url", cs.srv.URL, "--type", "relative")
	if code != 6 {
		t.Fatalf("exit code = %d, want 6 (validation); out:\n%s", code, out)
	}
	if !strings.Contains(out, "Growth ranking is in progress") {
		t.Errorf("error output missing surfaced API message; got:\n%s", out)
	}
}
