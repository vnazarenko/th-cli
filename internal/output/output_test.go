package output

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// timeoutErr is a net.Error whose Timeout() reports true, used to exercise the
// network-class mapping for transport timeouts not wrapped in a NetworkError.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestWriteJSONPrettyPrints(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]any{"a": 1, "b": "two"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output should end with a newline, got %q", out)
	}
	if !strings.Contains(out, "\n  \"a\": 1") {
		t.Errorf("output should be indented with two spaces, got:\n%s", out)
	}
	// Must round-trip back to the same data.
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestWriteJSONReindentsRawMessage(t *testing.T) {
	// A compact RawMessage must come out pretty-printed, not verbatim.
	raw := json.RawMessage(`{"report":{"status":"ready"},"x":[1,2]}`)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, raw); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\n    \"status\": \"ready\"") {
		t.Errorf("RawMessage should be re-indented, got:\n%s", out)
	}
}

func TestWriteJSONDoesNotEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]string{"url": "https://x/y?a=1&b=2"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(buf.String(), "a=1&b=2") {
		t.Errorf("ampersand should be emitted literally (HTML escaping off), got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "\\u0026") {
		t.Errorf("ampersand should not be unicode-escaped, got:\n%s", buf.String())
	}
}

// WriteJSON and WriteError must each only touch the writer they are handed —
// results to stdout, errors to stderr, never crossing.
func TestWriteRouting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := WriteJSON(&stdout, map[string]string{"ok": "yes"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if err := WriteError(&stderr, &APIError{Status: 401, Body: []byte(`{"error":"nope"}`)}); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	if strings.Contains(stdout.String(), "error") {
		t.Errorf("stdout should not contain the error envelope, got:\n%s", stdout.String())
	}
	if strings.Contains(stderr.String(), `"ok"`) {
		t.Errorf("stderr should not contain the result JSON, got:\n%s", stderr.String())
	}
}

func TestWriteErrorEnvelope(t *testing.T) {
	var buf bytes.Buffer
	err := &APIError{Status: 401, Body: []byte(`{"error":"unauthorized"}`)}
	if werr := WriteError(&buf, err); werr != nil {
		t.Fatalf("WriteError: %v", werr)
	}
	var env struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
		t.Fatalf("envelope is not valid JSON: %v", jerr)
	}
	if env.Error != "unauthorized" {
		t.Errorf("error field = %q, want the surfaced API message %q", env.Error, "unauthorized")
	}
	if !strings.Contains(env.Hint, "TRENDHERO_TOKEN") {
		t.Errorf("401 hint should mention TRENDHERO_TOKEN, got %q", env.Hint)
	}
}

func TestWriteErrorNilWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteError(&buf, nil); err != nil {
		t.Fatalf("WriteError(nil): %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("WriteError(nil) should write nothing, got %q", buf.String())
	}
}

func TestWriteErrorGenericHasNoHint(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteError(&buf, errors.New("boom")); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
		t.Fatalf("envelope is not valid JSON: %v", jerr)
	}
	if env["error"] != "boom" {
		t.Errorf("error field = %v, want %q", env["error"], "boom")
	}
	if _, ok := env["hint"]; ok {
		t.Errorf("generic error should omit the hint field, got %v", env["hint"])
	}
}

func TestAPIErrorMessageExtraction(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "error as string", body: `{"error":"plain message"}`, want: "plain message"},
		{name: "error as object with message", body: `{"error":{"message":"nested message","type":"x"}}`, want: "nested message"},
		{name: "top-level message fallback", body: `{"message":"top level"}`, want: "top level"},
		{name: "no error key", body: `{"foo":"bar"}`, want: ""},
		{name: "empty body", body: ``, want: ""},
		{name: "invalid json", body: `not json`, want: ""},
		{name: "null error", body: `{"error":null}`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &APIError{Status: 422, Body: []byte(tc.body)}
			if got := e.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExitCodeTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil is success", err: nil, want: ExitOK},
		{name: "generic error", err: errors.New("oops"), want: ExitGeneric},
		{name: "401 -> auth", err: &APIError{Status: 401}, want: ExitAuth},
		{name: "403 -> forbidden", err: &APIError{Status: 403}, want: ExitForbidden},
		{name: "404 -> not found", err: &APIError{Status: 404}, want: ExitNotFound},
		{name: "422 -> validation", err: &APIError{Status: 422}, want: ExitValidation},
		{name: "503 -> unavailable", err: &APIError{Status: 503}, want: ExitUnavailable},
		{name: "500 -> generic", err: &APIError{Status: 500}, want: ExitGeneric},
		{name: "network error -> network", err: &NetworkError{Err: errors.New("connection refused")}, want: ExitNetwork},
		{name: "network wraps deadline -> network", err: &NetworkError{Err: context.DeadlineExceeded}, want: ExitNetwork},
		{name: "bare deadline -> network", err: context.DeadlineExceeded, want: ExitNetwork},
		{name: "bare timeout net.Error -> network", err: timeoutErr{}, want: ExitNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// ExitCode must see through wrapping to find the typed cause.
func TestExitCodeThroughWrapping(t *testing.T) {
	err := fmt.Errorf("ordering report: %w", &APIError{Status: 403})
	if got := ExitCode(err); got != ExitForbidden {
		t.Errorf("ExitCode(wrapped 403) = %d, want %d", got, ExitForbidden)
	}
}

func TestAPIErrorErrorString(t *testing.T) {
	withMsg := &APIError{Status: 422, Body: []byte(`{"error":{"message":"not_enough_balance"}}`)}
	if !strings.Contains(withMsg.Error(), "not_enough_balance") {
		t.Errorf("Error() should include the API message, got %q", withMsg.Error())
	}
	if !strings.Contains(withMsg.Error(), "422") {
		t.Errorf("Error() should include the status, got %q", withMsg.Error())
	}
	noMsg := &APIError{Status: 503}
	if !strings.Contains(noMsg.Error(), "503") {
		t.Errorf("Error() without a body should still include the status, got %q", noMsg.Error())
	}
}

func TestNetworkErrorUnwrap(t *testing.T) {
	base := context.DeadlineExceeded
	ne := &NetworkError{Err: base}
	if !errors.Is(ne, context.DeadlineExceeded) {
		t.Errorf("NetworkError should unwrap to its cause")
	}
	if !strings.Contains(ne.Error(), "network error") {
		t.Errorf("NetworkError.Error() = %q, want it to mention 'network error'", ne.Error())
	}
}

// WriteError surfaces the API's own message (not a generic wrapper) for 422s.
func TestWriteErrorSurfacesAPIMessage(t *testing.T) {
	var buf bytes.Buffer
	err := &APIError{Status: 422, Body: []byte(`{"error":{"message":"not_enough_balance"}}`)}
	if werr := WriteError(&buf, err); werr != nil {
		t.Fatalf("WriteError: %v", werr)
	}
	var env struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
		t.Fatalf("envelope is not valid JSON: %v", jerr)
	}
	if env.Error != "not_enough_balance" {
		t.Errorf("error field = %q, want the surfaced API message %q", env.Error, "not_enough_balance")
	}
	if env.Hint == "" {
		t.Errorf("422 should carry a hint")
	}
}

// TestHintForEachClass asserts every non-generic exit class emits its own
// remediation hint (the guidance agents/users read on failure). Previously only
// the 401 and 422 hints were covered.
func TestHintForEachClass(t *testing.T) {
	cases := []struct {
		code    int
		wantSub string
	}{
		{ExitAuth, "TRENDHERO_TOKEN"},
		{ExitForbidden, "subscription"},
		{ExitNotFound, "report order"},
		{ExitNetwork, "connectivity"},
		{ExitValidation, "balance"},
		{ExitUnavailable, "unavailable"},
	}
	for _, tc := range cases {
		got := hintFor(tc.code)
		if got == "" {
			t.Errorf("hintFor(%d) is empty, want a hint mentioning %q", tc.code, tc.wantSub)
			continue
		}
		if !strings.Contains(got, tc.wantSub) {
			t.Errorf("hintFor(%d) = %q, want it to mention %q", tc.code, got, tc.wantSub)
		}
	}
	// Success and generic classes intentionally carry no hint.
	for _, code := range []int{ExitOK, ExitGeneric} {
		if got := hintFor(code); got != "" {
			t.Errorf("hintFor(%d) = %q, want empty", code, got)
		}
	}
}

// Guard against an accidentally slow path: ExitCode/Message must be allocation
// cheap and not block. This is a smoke check, not a benchmark.
func TestNoHang(t *testing.T) {
	done := make(chan struct{})
	go func() {
		_ = ExitCode(&APIError{Status: 422, Body: []byte(`{"error":"x"}`)})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ExitCode hung")
	}
}
