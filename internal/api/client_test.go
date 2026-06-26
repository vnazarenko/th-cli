package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vnazarenko/th-cli/internal/config"
	"github.com/vnazarenko/th-cli/internal/output"
)

// recordingServer captures the most recent request so tests can assert on the
// path, headers and query the wrapper produced, while returning a canned
// response chosen per-request by the handler.
type recordingServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []recordedRequest
}

type recordedRequest struct {
	method string
	path   string
	auth   string
	ua     string
	query  map[string][]string
}

// newRecordingServer starts an httptest server whose handler is `h`. Every
// request is recorded before `h` runs so assertions don't depend on the
// response path.
func newRecordingServer(t *testing.T, h http.HandlerFunc) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.reqs = append(rs.reqs, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			ua:     r.Header.Get("User-Agent"),
			query:  r.URL.Query(),
		})
		rs.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func (rs *recordingServer) last() recordedRequest {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.reqs[len(rs.reqs)-1]
}

func (rs *recordingServer) count() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.reqs)
}

// writeJSON is a small handler helper.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// newClient builds a wrapper pointed at the recording server. token == "" leaves
// the Bearer editor unregistered (top_profiles works without it).
func newClient(t *testing.T, rs *recordingServer, token string) *THClient {
	t.Helper()
	c, err := New(config.Config{BaseURL: rs.srv.URL, Token: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRequiresBaseURL(t *testing.T) {
	if _, err := New(config.Config{}); !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("New with empty BaseURL = %v, want ErrNoBaseURL", err)
	}
}

func TestTopProfilesPathHeadersAndQuery(t *testing.T) {
	rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `[{"username":"cristiano","pk":"1"}]`)
	})
	c := newClient(t, rs, "") // no token: top_profiles is unauthenticated

	year, month := 2026, 6
	typ := Absolute
	country := US
	raw, err := c.TopProfiles(context.Background(), &GetTopProfilesParams{
		Type:    &typ,
		Country: &country,
		Year:    &year,
		Month:   &month,
	})
	if err != nil {
		t.Fatalf("TopProfiles: %v", err)
	}

	// JSON passthrough: the raw body must round-trip into the typed array.
	var profiles []TopProfile
	if err := json.Unmarshal(raw, &profiles); err != nil {
		t.Fatalf("passthrough body not valid top_profiles JSON: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Username != "cristiano" {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}

	got := rs.last()
	if got.path != "/api/public/top_profiles" {
		t.Errorf("path = %q, want /api/public/top_profiles", got.path)
	}
	if got.ua != "th-cli/"+Version {
		t.Errorf("User-Agent = %q, want th-cli/%s", got.ua, Version)
	}
	// No token configured → no Authorization header.
	if got.auth != "" {
		t.Errorf("Authorization = %q, want empty (top_profiles is unauthenticated)", got.auth)
	}
	for k, want := range map[string]string{"type": "absolute", "country": "US", "year": "2026", "month": "6"} {
		if v := got.query[k]; len(v) != 1 || v[0] != want {
			t.Errorf("query %q = %v, want [%q]", k, v, want)
		}
	}
}

func TestTopProfilesSendsTokenWhenPresent(t *testing.T) {
	rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `[]`)
	})
	c := newClient(t, rs, "secret-token")

	if _, err := c.TopProfiles(context.Background(), &GetTopProfilesParams{}); err != nil {
		t.Fatalf("TopProfiles: %v", err)
	}
	if got := rs.last().auth; got != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", got)
	}
}

func TestGetReportPathAndBearer(t *testing.T) {
	const ready = `{"type":"full","report":{"status":"ready","username":"cristiano"}}`
	rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, ready)
	})
	c := newClient(t, rs, "tok")

	raw, err := c.GetReport(context.Background(), "cristiano")
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if string(raw) != ready {
		t.Errorf("GetReport body = %q, want passthrough %q", raw, ready)
	}

	got := rs.last()
	if got.method != http.MethodGet {
		t.Errorf("method = %s, want GET", got.method)
	}
	if got.path != "/api/public/v1/reports/cristiano" {
		t.Errorf("path = %q, want /api/public/v1/reports/cristiano", got.path)
	}
	if got.auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", got.auth)
	}
}

func TestOrderReportPostsUsernameQuery(t *testing.T) {
	rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"report":{"status":"collecting"}}`)
	})
	c := newClient(t, rs, "tok")

	if _, err := c.OrderReport(context.Background(), "leomessi"); err != nil {
		t.Fatalf("OrderReport: %v", err)
	}

	got := rs.last()
	if got.method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.method)
	}
	if got.path != "/api/public/v1/reports" {
		t.Errorf("path = %q, want /api/public/v1/reports", got.path)
	}
	if v := got.query["username"]; len(v) != 1 || v[0] != "leomessi" {
		t.Errorf("username query = %v, want [leomessi]", v)
	}
	if got.auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", got.auth)
	}
}

// TestErrorStatusMapping asserts each non-2xx status becomes an APIError whose
// exit-code class matches the plan, with the API message surfaced verbatim.
func TestErrorStatusMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantExit int
		wantMsg  string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":"unauthorized"}`, output.ExitAuth, "unauthorized"},
		{"forbidden", http.StatusForbidden, `{"error":"not_enough_balance"}`, output.ExitForbidden, "not_enough_balance"},
		{"notfound", http.StatusNotFound, `{"error":"report not found"}`, output.ExitNotFound, "report not found"},
		{"validation", http.StatusUnprocessableEntity, `{"error":{"message":"impossible_report"}}`, output.ExitValidation, "impossible_report"},
		{"unavailable", http.StatusServiceUnavailable, `{"error":"try later"}`, output.ExitUnavailable, "try later"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, tc.status, tc.body)
			})
			c := newClient(t, rs, "tok")

			_, err := c.GetReport(context.Background(), "x")
			if err == nil {
				t.Fatalf("expected error for status %d", tc.status)
			}
			var apiErr *output.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %T, want *output.APIError", err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, tc.status)
			}
			if output.ExitCode(err) != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", output.ExitCode(err), tc.wantExit)
			}
			if apiErr.Message() != tc.wantMsg {
				t.Errorf("Message = %q, want %q", apiErr.Message(), tc.wantMsg)
			}
		})
	}
}

func TestReportStatusExtractsNestedStatus(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"nested ready", `{"preview":{"status":"x"},"report":{"status":"ready"}}`, "ready"},
		{"nested collecting", `{"report":{"status":"collecting"}}`, "collecting"},
		{"top-level status ignored", `{"status":"ready"}`, ""},
		{"missing report", `{"type":"full"}`, ""},
		{"garbage", `not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportStatus(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("reportStatus(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// fakeClock makes WaitForReport deterministic: now() reads a shared instant and
// sleep() advances it, so no real time passes.
type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) sleep(_ context.Context, d time.Duration) error {
	f.t = f.t.Add(d)
	return nil
}

// statusSequenceServer returns each body in order, repeating the last one once
// exhausted — used to script collecting→ready style polls.
func statusSequenceServer(t *testing.T, bodies ...string) *recordingServer {
	t.Helper()
	var i int
	var mu sync.Mutex
	return newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		body := bodies[i]
		if i < len(bodies)-1 {
			i++
		}
		mu.Unlock()
		writeJSON(w, http.StatusOK, body)
	})
}

func installClock(c *THClient) *fakeClock {
	clk := &fakeClock{t: time.Unix(0, 0)}
	c.now = clk.now
	c.sleep = clk.sleep
	return clk
}

func TestWaitForReportCollectingThenReady(t *testing.T) {
	rs := statusSequenceServer(t,
		`{"report":{"status":"collecting"}}`,
		`{"report":{"status":"ready","username":"cristiano"}}`,
	)
	c := newClient(t, rs, "tok")
	installClock(c)

	raw, err := c.WaitForReport(context.Background(), "cristiano", time.Second, time.Minute)
	if err != nil {
		t.Fatalf("WaitForReport: %v", err)
	}
	if reportStatus(raw) != "ready" {
		t.Errorf("final status = %q, want ready", reportStatus(raw))
	}
	if rs.count() != 2 {
		t.Errorf("polled %d times, want 2 (collecting then ready)", rs.count())
	}
}

func TestWaitForReportCollectingThenImpossible(t *testing.T) {
	rs := statusSequenceServer(t,
		`{"report":{"status":"collecting"}}`,
		`{"report":{"status":"impossible"}}`,
	)
	c := newClient(t, rs, "tok")
	installClock(c)

	raw, err := c.WaitForReport(context.Background(), "x", time.Second, time.Minute)
	if err != nil {
		t.Fatalf("WaitForReport: %v", err)
	}
	// impossible is terminal: no error, the blob is returned for the caller to
	// print (the command exits 0 and the agent reads report.status).
	if reportStatus(raw) != "impossible" {
		t.Errorf("final status = %q, want impossible", reportStatus(raw))
	}
}

func TestWaitForReportTimeout(t *testing.T) {
	rs := statusSequenceServer(t, `{"report":{"status":"collecting"}}`) // never terminal
	c := newClient(t, rs, "tok")
	installClock(c)

	_, err := c.WaitForReport(context.Background(), "x", 10*time.Second, 30*time.Second)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	// A timeout maps to the network/timeout exit class.
	if output.ExitCode(err) != output.ExitNetwork {
		t.Errorf("ExitCode = %d, want %d (network/timeout)", output.ExitCode(err), output.ExitNetwork)
	}
	var netErr *output.NetworkError
	if !errors.As(err, &netErr) {
		t.Errorf("error = %T, want *output.NetworkError", err)
	}
}

// TestWaitForReportTimeoutBoundsSlowPoll proves the timeout is a hard ceiling on
// the whole wait, including an in-flight request — not just a between-polls
// check. The server never responds; the overall timeout must cancel the poll and
// return promptly. Without the context deadline on the loop, a single slow poll
// would instead block up to the 30s per-request timeout. Uses the real clock so
// the wrapped context (not the injected fake clock) does the bounding.
func TestWaitForReportTimeoutBoundsSlowPoll(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rs := newRecordingServer(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done(): // client cancelled (deadline hit) — stop blocking
		case <-release:
		}
	})
	c := newClient(t, rs, "tok")

	start := time.Now()
	_, err := c.WaitForReport(context.Background(), "x", time.Second, 50*time.Millisecond)
	elapsed := time.Since(start)

	if output.ExitCode(err) != output.ExitNetwork {
		t.Errorf("ExitCode = %d, want %d (network/timeout)", output.ExitCode(err), output.ExitNetwork)
	}
	if elapsed > 5*time.Second {
		t.Errorf("WaitForReport blocked %s; the overall timeout must bound the in-flight poll", elapsed)
	}
}

// TestWaitForReportPerRequestTimeoutNotRelabeled proves a single slow poll that
// trips the per-request http.Client timeout — while the overall --wait budget is
// still alive — is surfaced as its own error, not relabeled as the overall
// "timed out after <timeout>" message. In modern Go the per-request timeout also
// satisfies errors.Is(err, context.DeadlineExceeded), so WaitForReport must key
// the rewrite off the wait context's own deadline (ctx.Err()), not the error.
func TestWaitForReportPerRequestTimeoutNotRelabeled(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rs := newRecordingServer(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done(): // per-request timeout cancelled the request
		case <-release:
		}
	})

	// Per-request http.Client timeout (50ms) far shorter than the overall wait
	// budget (1 minute): the per-request timeout fires first, with the wait
	// context still alive.
	gen, err := NewClientWithResponses(rs.srv.URL+apiPrefix,
		WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}),
		WithRequestEditorFn(userAgentEditor))
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}
	c := &THClient{gen: gen, timeout: DefaultRequestTimeout, now: time.Now, sleep: sleepCtx}

	start := time.Now()
	_, err = c.WaitForReport(context.Background(), "x", time.Second, time.Minute)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from the slow poll")
	}
	// Still a network/timeout-class failure (exit 5)…
	if output.ExitCode(err) != output.ExitNetwork {
		t.Errorf("ExitCode = %d, want %d (network/timeout)", output.ExitCode(err), output.ExitNetwork)
	}
	// …but NOT relabeled as the overall --wait timeout (the budget is still alive).
	if strings.Contains(err.Error(), "timed out after") {
		t.Errorf("per-request timeout was relabeled as the overall wait timeout: %v", err)
	}
	// And it ended on the per-request timeout, not after the 1m budget.
	if elapsed > 5*time.Second {
		t.Errorf("WaitForReport blocked %s; the per-request timeout should end it promptly", elapsed)
	}
}

// TestTransportErrorMapsToNetwork covers the non-timeout transport-failure
// branch in do(): a connection that cannot be established becomes a
// *output.NetworkError (exit class 5), distinct from the API-error and
// timeout paths exercised elsewhere. Agents branch on exit 5 to retry on
// connectivity, so this mapping is contractually important.
func TestTransportErrorMapsToNetwork(t *testing.T) {
	// Start a server only to claim a real URL, then close it so the address
	// refuses connections.
	rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	url := rs.srv.URL
	rs.srv.Close()

	c, err := New(config.Config{BaseURL: url, Token: "tok"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.GetReport(context.Background(), "x")
	if err == nil {
		t.Fatal("expected a transport error against a closed server")
	}
	var netErr *output.NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("error = %T, want *output.NetworkError", err)
	}
	if output.ExitCode(err) != output.ExitNetwork {
		t.Errorf("ExitCode = %d, want %d (network)", output.ExitCode(err), output.ExitNetwork)
	}
}

// TestSleepCtx covers the production poll sleeper directly (WaitForReport tests
// inject a fake clock, so this branch is otherwise unexercised). A cancelled
// context must return its error promptly; an uncancelled one must return nil
// after the interval.
func TestSleepCtx(t *testing.T) {
	t.Run("cancelled returns ctx error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Errorf("sleepCtx(cancelled) = %v, want context.Canceled", err)
		}
	})
	t.Run("elapses and returns nil", func(t *testing.T) {
		if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
			t.Errorf("sleepCtx(short) = %v, want nil", err)
		}
	})
}

// TestWaitForReportCancelledSleepMapsToNetwork verifies that a cancellation
// surfacing through the poll sleep is wrapped as a network/timeout-class error
// (exit 5), matching how a real Ctrl-C during --wait is reported.
func TestWaitForReportCancelledSleepMapsToNetwork(t *testing.T) {
	rs := statusSequenceServer(t, `{"report":{"status":"collecting"}}`) // never terminal
	c := newClient(t, rs, "tok")
	c.now = func() time.Time { return time.Unix(0, 0) } // never reach the deadline
	c.sleep = func(context.Context, time.Duration) error { return context.Canceled }

	_, err := c.WaitForReport(context.Background(), "x", time.Second, time.Minute)
	if output.ExitCode(err) != output.ExitNetwork {
		t.Errorf("ExitCode = %d, want %d (cancelled poll)", output.ExitCode(err), output.ExitNetwork)
	}
}

func TestWaitForReportPropagatesAPIError(t *testing.T) {
	rs := newRecordingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"error":"report not found"}`)
	})
	c := newClient(t, rs, "tok")
	installClock(c)

	_, err := c.WaitForReport(context.Background(), "x", time.Second, time.Minute)
	if output.ExitCode(err) != output.ExitNotFound {
		t.Errorf("ExitCode = %d, want %d (404 propagated)", output.ExitCode(err), output.ExitNotFound)
	}
}
