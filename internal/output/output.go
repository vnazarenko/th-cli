// Package output centralizes how the `th-cli` CLI emits results and failures.
//
// Successful results are written as pretty-printed JSON to stdout via WriteJSON
// so agents can parse them directly. Failures are written as a small JSON
// envelope — `{"error": ..., "hint": ...}` — to stderr via WriteError, and the
// process exit code is derived from the error's class via ExitCode. Together
// these let automation branch on the exit code and read a stable error shape
// without scraping free-form text.
package output

import (
	"encoding/json"
	"errors"
	"io"
)

// WriteJSON writes v as indented JSON followed by a newline. HTML escaping is
// disabled so URLs (e.g. signed avatar links) render literally. Values that are
// already json.RawMessage are re-indented to match. Intended destination is
// stdout; the writer is a parameter so callers and tests can redirect it.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// errorEnvelope is the on-the-wire shape WriteError emits. Hint is omitted when
// empty so generic errors stay terse.
type errorEnvelope struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

// WriteError writes err as a JSON `{"error":...,"hint":...}` envelope followed
// by a newline. The "error" field carries the API's own message when err is an
// *APIError that surfaced one, otherwise err.Error(). The "hint" field offers
// class-specific guidance (e.g. how to set a token on 401). Intended
// destination is stderr; the writer is a parameter for redirection/testing. A
// nil error writes nothing.
func WriteError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	env := errorEnvelope{
		Error: displayMessage(err),
		Hint:  hintFor(ExitCode(err)),
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// displayMessage prefers the API's own error message when err is an *APIError
// that carries one; this keeps the surfaced text identical to what the server
// reported (e.g. "not_enough_balance"). Otherwise it falls back to err.Error().
func displayMessage(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if msg := apiErr.Message(); msg != "" {
			return msg
		}
	}
	return err.Error()
}

// hintFor returns class-specific remediation guidance for an exit code, or ""
// when no useful hint exists (generic/success).
func hintFor(code int) string {
	switch code {
	case ExitAuth:
		return "set TRENDHERO_TOKEN (or pass --token); mint an AccessToken at " +
			"/app/api/access-tokens (requires the AdvancedApi subscription)"
	case ExitForbidden:
		return "the token is valid but not permitted for this request — check your " +
			"subscription/feature access or account balance"
	case ExitNotFound:
		return "no resource found — for reports, order one first with " +
			"`th-cli report order <username>`"
	case ExitNetwork:
		return "network or timeout error — check connectivity and the configured " +
			"base URL (--base-url or TRENDHERO_BASE_URL)"
	case ExitValidation:
		return "the API rejected the request (validation or insufficient balance) — " +
			"see the error message above"
	case ExitUnavailable:
		return "the service is temporarily unavailable — retry shortly"
	default:
		return ""
	}
}
