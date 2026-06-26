package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// Process exit codes. Each non-zero code encodes a failure *class* so that
// automation (agents) can branch on the result without parsing stderr text.
// The mapping is fixed by the plan's Technical Details section.
const (
	ExitOK          = 0 // success
	ExitGeneric     = 1 // usage error or any failure with no more specific class
	ExitAuth        = 2 // HTTP 401 — missing/invalid token
	ExitForbidden   = 3 // HTTP 403 — token valid but not permitted
	ExitNotFound    = 4 // HTTP 404 — no such resource
	ExitNetwork     = 5 // transport failure or timeout (no HTTP response)
	ExitValidation  = 6 // HTTP 422 — validation / paid-action rejection (e.g. not_enough_balance)
	ExitUnavailable = 7 // HTTP 503 — service temporarily unavailable
)

// APIError represents a non-2xx HTTP response from the public API. The raw
// response Body is preserved so the API's own error message can be surfaced
// verbatim; Status drives the exit-code class.
type APIError struct {
	Status int    // HTTP status code of the response
	Body   []byte // raw response body (may be empty)
}

// Error implements the error interface. It folds in the API's message when one
// can be extracted so logs/tests get a useful single-line description.
func (e *APIError) Error() string {
	if msg := e.Message(); msg != "" {
		return fmt.Sprintf("API error (HTTP %d): %s", e.Status, msg)
	}
	return fmt.Sprintf("API error (HTTP %d)", e.Status)
}

// Message extracts the human-readable error string from the response body. The
// public API renders errors as `{ "error": ... }` where `error` is either a
// plain string or an object carrying `message`. As a defensive fallback a
// top-level `message` is also honored. Returns "" when nothing usable is found.
func (e *APIError) Message() string {
	if len(e.Body) == 0 {
		return ""
	}

	var env struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(e.Body, &env); err != nil {
		return ""
	}

	if len(env.Error) > 0 {
		// `error` as a plain string: {"error": "some message"}.
		var s string
		if err := json.Unmarshal(env.Error, &s); err == nil && s != "" {
			return s
		}
		// `error` as an object: {"error": {"message": "...", "type": "..."}}.
		var obj struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(env.Error, &obj); err == nil && obj.Message != "" {
			return obj.Message
		}
	}

	// Fallback: a top-level {"message": "..."} envelope.
	return env.Message
}

// exitCode maps the HTTP status to its failure class. Statuses outside the
// known set collapse to the generic code.
func (e *APIError) exitCode() int {
	switch e.Status {
	case http.StatusUnauthorized: // 401
		return ExitAuth
	case http.StatusForbidden: // 403
		return ExitForbidden
	case http.StatusNotFound: // 404
		return ExitNotFound
	case http.StatusUnprocessableEntity: // 422
		return ExitValidation
	case http.StatusServiceUnavailable: // 503
		return ExitUnavailable
	default:
		return ExitGeneric
	}
}

// NetworkError wraps a transport-level failure (connection refused, DNS, TLS,
// or a context deadline/timeout) — i.e. any failure where no HTTP response was
// received. It always maps to ExitNetwork.
type NetworkError struct {
	Err error
}

func (e *NetworkError) Error() string {
	return "network error: " + e.Err.Error()
}

// Unwrap exposes the underlying cause so errors.Is/As can inspect it (e.g.
// context.DeadlineExceeded).
func (e *NetworkError) Unwrap() error {
	return e.Err
}

// ExitCoder is implemented by errors that know their own failure-class exit
// code. ExitCode honors it first, so error types defined in other packages
// (e.g. config.MissingTokenError, which maps to ExitAuth) can declare their
// class without output importing those packages.
type ExitCoder interface {
	ExitCode() int
}

// ExitCode maps an error to the process exit code encoding its failure class.
// nil → ExitOK. Errors implementing ExitCoder report their own class; APIErrors
// map by HTTP status; NetworkErrors and bare timeouts/deadlines map to
// ExitNetwork; anything else is generic.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}

	var coder ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.exitCode()
	}

	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return ExitNetwork
	}

	// A timeout/deadline that was not wrapped in a NetworkError still belongs
	// to the network class.
	if isTimeout(err) {
		return ExitNetwork
	}

	return ExitGeneric
}

// isTimeout reports whether err is (or wraps) a context deadline or a
// timeout-flavored net.Error.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
