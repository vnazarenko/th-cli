package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// reportStatusFromOutput parses the command's JSON stdout and returns the nested
// report.status, mirroring where the CLI itself reads status for --wait.
func reportStatusFromOutput(t *testing.T, out string) string {
	t.Helper()
	var env struct {
		Report struct {
			Status string `json:"status"`
		} `json:"report"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid report JSON: %v\nout:\n%s", err, out)
	}
	return env.Report.Status
}

// sequenceHandler returns each body in order, repeating the last once exhausted
// — used to script `collecting`→`ready` style polls for --wait tests.
func sequenceHandler(bodies ...string) http.HandlerFunc {
	var (
		mu sync.Mutex
		i  int
	)
	return func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		body := bodies[i]
		if i < len(bodies)-1 {
			i++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

const (
	reportReady      = `{"type":"full","report":{"status":"ready","username":"cristiano"}}`
	reportCollecting = `{"type":"full","report":{"status":"collecting","username":"cristiano"}}`
	reportImpossible = `{"type":"full","report":{"status":"impossible","username":"cristiano"}}`
)

func TestReportGetMissingToken(t *testing.T) {
	isolateEnv(t)
	// Guard server: the token check must short-circuit before any HTTP call.
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	out, code := runCaptured(t, "report", "get", "cristiano", "--base-url", cs.srv.URL)
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

func TestReportGetReadyPassthrough(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reportReady))
	})

	out, code := runCaptured(t, "report", "get", "cristiano", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}

	req := cs.last()
	if req.path != "/api/public/v1/reports/cristiano" {
		t.Errorf("request path = %q, want /api/public/v1/reports/cristiano", req.path)
	}
	if req.auth != "Bearer tok" {
		t.Errorf("Authorization header = %q, want %q", req.auth, "Bearer tok")
	}
	if got := reportStatusFromOutput(t, out); got != "ready" {
		t.Errorf("report.status = %q, want ready", got)
	}
}

func TestReportGetCollectingPassthrough(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reportCollecting))
	})

	// Without --wait the current (still collecting) report is returned as-is.
	out, code := runCaptured(t, "report", "get", "cristiano", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}
	if cs.count() != 1 {
		t.Errorf("server called %d time(s); want 1 (no polling without --wait)", cs.count())
	}
	if got := reportStatusFromOutput(t, out); got != "collecting" {
		t.Errorf("report.status = %q, want collecting", got)
	}
}

func TestReportGetWaitCollectingThenReady(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, sequenceHandler(reportCollecting, reportReady))

	out, code := runCaptured(t, "report", "get", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok",
		"--wait", "--interval", "1ms", "--timeout", "5s")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}
	if cs.count() < 2 {
		t.Errorf("server polled %d time(s); want >= 2 (collecting then ready)", cs.count())
	}
	if got := reportStatusFromOutput(t, out); got != "ready" {
		t.Errorf("final report.status = %q, want ready", got)
	}
}

func TestReportGetWaitCollectingThenImpossible(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, sequenceHandler(reportCollecting, reportImpossible))

	// impossible is terminal-failed but still a success exit: the JSON is printed
	// and the agent reads report.status.
	out, code := runCaptured(t, "report", "get", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok",
		"--wait", "--interval", "1ms", "--timeout", "5s")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (impossible is terminal); out:\n%s", code, out)
	}
	if got := reportStatusFromOutput(t, out); got != "impossible" {
		t.Errorf("final report.status = %q, want impossible", got)
	}
}

func TestReportGetWaitTimeout(t *testing.T) {
	isolateEnv(t)
	// Never reaches a terminal status → --wait must time out (network class).
	cs := newCmdServer(t, sequenceHandler(reportCollecting))

	out, code := runCaptured(t, "report", "get", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok",
		"--wait", "--interval", "1ms", "--timeout", "15ms")
	if code != 5 {
		t.Fatalf("exit code = %d, want 5 (network/timeout); out:\n%s", code, out)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("error output missing timeout message; got:\n%s", out)
	}
}

func TestReportGet404(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"report not found"}`))
	})

	out, code := runCaptured(t, "report", "get", "nobody", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 4 {
		t.Fatalf("exit code = %d, want 4 (not found); out:\n%s", code, out)
	}
	if !strings.Contains(out, "report not found") {
		t.Errorf("error output missing surfaced API message; got:\n%s", out)
	}
}

func TestReportGet422(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"message":"impossible_report","type":"ImpossibleReportError"}}`))
	})

	out, code := runCaptured(t, "report", "get", "ghost", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 6 {
		t.Fatalf("exit code = %d, want 6 (validation); out:\n%s", code, out)
	}
	if !strings.Contains(out, "impossible_report") {
		t.Errorf("error output missing surfaced API message; got:\n%s", out)
	}
}
