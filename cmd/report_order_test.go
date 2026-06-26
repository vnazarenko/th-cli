package cmd

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vnazarenko/th-cli/internal/config"
)

// TestReportOrderRefusesWithoutConfirm verifies the paid-action guard: without
// --confirm (and without TRENDHERO_ALLOW_WRITES) the command must refuse with a
// clear "spends credits" message and never touch the API — an accidental order
// can never cost real credits.
func TestReportOrderRefusesWithoutConfirm(t *testing.T) {
	isolateEnv(t)
	// Fail loudly if the order ever reaches the server.
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	out, code := runCaptured(t, "report", "order", "cristiano", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (refused, no confirm); out:\n%s", code, out)
	}
	if cs.count() != 0 {
		t.Errorf("server called %d time(s); want 0 (must not order without confirm)", cs.count())
	}
	if !strings.Contains(out, "--confirm") || !strings.Contains(out, "credits") {
		t.Errorf("error output missing the spends-credits/--confirm guidance; got:\n%s", out)
	}
}

// TestReportOrderMissingToken verifies the token guard short-circuits before the
// confirm check or any network call.
func TestReportOrderMissingToken(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	out, code := runCaptured(t, "report", "order", "cristiano", "--base-url", cs.srv.URL, "--confirm")
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

// TestReportOrderWithConfirm verifies that with --confirm the command POSTs the
// order to /v1/reports with the Bearer token and username, and passes the
// resulting (collecting) report straight through.
func TestReportOrderWithConfirm(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reportCollecting))
	})

	out, code := runCaptured(t, "report", "order", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok", "--confirm")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}
	if cs.count() != 1 {
		t.Fatalf("server called %d time(s); want 1 (single order POST)", cs.count())
	}

	req := cs.last()
	if req.method != http.MethodPost {
		t.Errorf("request method = %q, want POST", req.method)
	}
	if req.path != "/api/public/v1/reports" {
		t.Errorf("request path = %q, want /api/public/v1/reports", req.path)
	}
	if req.auth != "Bearer tok" {
		t.Errorf("Authorization header = %q, want %q", req.auth, "Bearer tok")
	}
	if got := req.query.Get("username"); got != "cristiano" {
		t.Errorf("username query = %q, want cristiano", got)
	}
	if got := reportStatusFromOutput(t, out); got != "collecting" {
		t.Errorf("report.status = %q, want collecting", got)
	}
}

// TestReportOrderAllowWritesEnv verifies TRENDHERO_ALLOW_WRITES=1 is an accepted
// alternative to --confirm (the standing opt-in for unattended agents).
func TestReportOrderAllowWritesEnv(t *testing.T) {
	isolateEnv(t)
	t.Setenv(config.EnvAllowWrites, "1")
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reportCollecting))
	})

	out, code := runCaptured(t, "report", "order", "cristiano", "--base-url", cs.srv.URL, "--token", "tok")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (env opt-in); out:\n%s", code, out)
	}
	if cs.count() != 1 {
		t.Errorf("server called %d time(s); want 1 (order placed via env opt-in)", cs.count())
	}
}

// TestReportOrderConfirmWaitCollectingThenReady verifies --confirm --wait places
// the order (collecting) and then polls until the report is ready, exiting 0
// with the final report JSON.
func TestReportOrderConfirmWaitCollectingThenReady(t *testing.T) {
	isolateEnv(t)
	// POST order → collecting; subsequent GET polls → ready (terminal).
	cs := newCmdServer(t, sequenceHandler(reportCollecting, reportReady))

	out, code := runCaptured(t, "report", "order", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok",
		"--confirm", "--wait", "--interval", "1ms", "--timeout", "5s")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out:\n%s", code, out)
	}
	if cs.count() < 2 {
		t.Errorf("server called %d time(s); want >= 2 (order then poll)", cs.count())
	}
	if got := reportStatusFromOutput(t, out); got != "ready" {
		t.Errorf("final report.status = %q, want ready", got)
	}
}

// TestReportOrder422NotEnoughBalance verifies a paid-action rejection surfaces
// the API message and maps to exit 6 (validation/paid).
func TestReportOrder422NotEnoughBalance(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"message":"not_enough_balance","type":"NotEnoughBalanceError"}}`))
	})

	out, code := runCaptured(t, "report", "order", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok", "--confirm")
	if code != 6 {
		t.Fatalf("exit code = %d, want 6 (validation/paid); out:\n%s", code, out)
	}
	if !strings.Contains(out, "not_enough_balance") {
		t.Errorf("error output missing surfaced API message; got:\n%s", out)
	}
}

// TestReportOrder403 verifies a forbidden response maps to exit 3.
func TestReportOrder403(t *testing.T) {
	isolateEnv(t)
	cs := newCmdServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"not permitted for this token"}`))
	})

	out, code := runCaptured(t, "report", "order", "cristiano",
		"--base-url", cs.srv.URL, "--token", "tok", "--confirm")
	if code != 3 {
		t.Fatalf("exit code = %d, want 3 (forbidden); out:\n%s", code, out)
	}
	if !strings.Contains(out, "not permitted") {
		t.Errorf("error output missing surfaced API message; got:\n%s", out)
	}
}
