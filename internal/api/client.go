// This file is the hand-written, high-level half of package api. The other half
// (client.gen.go) is generated from public-api.openapi.yaml and exposes a raw
// HTTP client (the generated Client type, NewGetReportRequest, etc.). Here we
// wrap that low-level client with the cross-cutting behavior every CLI call
// needs and expose a small, JSON-passthrough surface to the cmd layer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vnazarenko/th-cli/internal/config"
	"github.com/vnazarenko/th-cli/internal/output"
)

// Version and Commit carry build metadata. Version is reported in the
// User-Agent header (`th-cli/<Version>`) and, alongside Commit, by `th-cli version`.
// They default to "dev"/"none" and are overridden at build time via -ldflags
// (see the Makefile). They live here — not in cmd — so the User-Agent can read
// Version without cmd importing api creating a cycle (cmd already imports api),
// keeping a single source of truth that `th-cli version` reuses.
var (
	Version = "dev"
	Commit  = "none"
)

// apiPrefix is appended once to the configured host to form the generated
// client's server. With it set here, spec paths stay clean: `/top_profiles`
// (no v1) and `/v1/reports*` share this single prefix with no double `/api`.
const apiPrefix = "/api/public"

// Default per-request timeout and report-polling cadence. All are overridable:
// the request timeout via New's http.Client, the poll interval/timeout via
// WaitForReport's arguments.
const (
	// DefaultRequestTimeout bounds every single HTTP call, not just --wait.
	DefaultRequestTimeout = 30 * time.Second
	// DefaultPollInterval is the gap between report status polls.
	DefaultPollInterval = 10 * time.Second
	// DefaultPollTimeout is the overall budget for WaitForReport.
	DefaultPollTimeout = 5 * time.Minute
)

// ErrNoBaseURL is returned by New when no API host is configured. Without a host
// the client has nowhere to send requests; the caller (a command) surfaces this
// before any network attempt.
var ErrNoBaseURL = errors.New(
	"no API host configured: pass --base-url or set " + config.EnvBaseURL)

// reportTerminalStatuses are the report.status values at which WaitForReport
// stops polling. `ready`, `recollecting` and `impossibleButReady` are all
// usable/terminal; `impossible` is terminal-failed. Only `collecting` (and any
// initial/pending state) keeps the poll loop going.
var reportTerminalStatuses = map[string]bool{
	"ready":              true,
	"recollecting":       true,
	"impossibleButReady": true,
	"impossible":         true,
}

// THClient is the high-level trendHERO public-API client used by the cmd layer.
// It wraps the generated low-level client (ClientWithResponses) with a fixed
// {host}/api/public server, a default per-request timeout, a User-Agent header,
// and an optional Bearer token (attached only when configured, since
// top_profiles is unauthenticated). now/sleep are injection seams so the
// WaitForReport poll loop is deterministic in tests.
//
// It is named THClient rather than Client because the generated code already
// exports a low-level Client type in this package.
type THClient struct {
	gen     *ClientWithResponses
	timeout time.Duration
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

// New builds a THClient for the resolved config. It sets the generated client's
// server to {host}/api/public, installs a default http.Client timeout, and
// registers request editors for the User-Agent and (when a token is present)
// the Bearer header. It returns ErrNoBaseURL when no host was configured.
func New(cfg config.Config) (*THClient, error) {
	if cfg.BaseURL == "" {
		return nil, ErrNoBaseURL
	}

	opts := []ClientOption{
		WithHTTPClient(&http.Client{Timeout: DefaultRequestTimeout}),
		WithRequestEditorFn(userAgentEditor),
	}
	if cfg.Token != "" {
		opts = append(opts, WithRequestEditorFn(bearerEditor(cfg.Token)))
	}

	gen, err := NewClientWithResponses(cfg.BaseURL+apiPrefix, opts...)
	if err != nil {
		return nil, err
	}

	return &THClient{
		gen:     gen,
		timeout: DefaultRequestTimeout,
		now:     time.Now,
		sleep:   sleepCtx,
	}, nil
}

// userAgentEditor stamps `User-Agent: th-cli/<Version>` on every request.
func userAgentEditor(_ context.Context, req *http.Request) error {
	req.Header.Set("User-Agent", "th-cli/"+Version)
	return nil
}

// bearerEditor returns an editor that attaches `Authorization: Bearer <token>`.
// It is only registered when a token is configured.
func bearerEditor(token string) RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

// TopProfiles fetches the ranked top-profiles list. The token is optional for
// this endpoint (it is sent only if one was configured). Returns the response
// body as raw JSON on success.
func (c *THClient) TopProfiles(ctx context.Context, params *GetTopProfilesParams) (json.RawMessage, error) {
	return c.do(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.gen.GetTopProfiles(ctx, params)
	})
}

// GetReport fetches a single influencer report by username. Requires a Bearer
// token server-side (callers enforce token presence via config.RequireToken).
func (c *THClient) GetReport(ctx context.Context, username string) (json.RawMessage, error) {
	return c.do(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.gen.GetReport(ctx, username)
	})
}

// OrderReport orders (pays for) a report for the given username. The paid action
// is guarded at the command layer; this method just issues the POST.
func (c *THClient) OrderReport(ctx context.Context, username string) (json.RawMessage, error) {
	return c.do(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.gen.CreateReport(ctx, &CreateReportParams{Username: username})
	})
}

// do runs one HTTP call, applies a per-request timeout when the context has no
// deadline, reads the body, and classifies the result: a 2xx becomes raw JSON;
// a non-2xx becomes an *output.APIError (preserving the body so the API's own
// error message can be surfaced); a transport/timeout failure becomes an
// *output.NetworkError.
func (c *THClient) do(ctx context.Context, call func(context.Context) (*http.Response, error)) (json.RawMessage, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	resp, err := call(ctx)
	if err != nil {
		return nil, &output.NetworkError{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &output.NetworkError{Err: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &output.APIError{Status: resp.StatusCode, Body: body}
	}
	return json.RawMessage(body), nil
}

// withTimeout adds a default timeout to ctx unless it already carries a deadline
// (e.g. one a caller set explicitly). The returned cancel is always safe to
// call.
func (c *THClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}

// WaitForReport polls GetReport until the report reaches a terminal status
// (ready / recollecting / impossibleButReady / impossible) or the timeout
// elapses, returning the final raw report JSON. interval/timeout fall back to
// the package defaults when non-positive. A timeout yields an *output.NetworkError
// (exit class "network/timeout"); any API error from GetReport propagates
// unchanged so its status maps to the right exit code.
//
// timeout is a hard ceiling on the whole wait, not just a between-polls check:
// the loop runs under a context carrying that deadline, so an in-flight
// GetReport is bounded by the remaining budget (withTimeout honors the existing
// deadline) and a slow poll cannot overrun timeout. The explicit now()-based
// check below still produces the user-facing timeout message and keeps the loop
// deterministic under the injected clock.
func (c *THClient) WaitForReport(ctx context.Context, username string, interval, timeout time.Duration) (json.RawMessage, error) {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	if timeout <= 0 {
		timeout = DefaultPollTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// timedOut is the user-facing timeout result. It is returned both from the
	// explicit deadline check (the common case under the injected clock) and when
	// our own context deadline fires mid-poll, so a slow in-flight request reads
	// as a clean timeout rather than a raw "context deadline exceeded".
	timedOut := func() error {
		return &output.NetworkError{Err: fmt.Errorf(
			"timed out after %s waiting for report %q to reach a terminal status", timeout, username)}
	}

	deadline := c.now().Add(timeout)
	for {
		raw, err := c.GetReport(ctx, username)
		if err != nil {
			// Only the wait context's *own* deadline means the --wait budget is
			// spent. We must check ctx.Err(), not errors.Is(err, …): the 30s
			// per-request http.Client timeout also surfaces as a
			// context.DeadlineExceeded (Go wraps "Client.Timeout exceeded" that
			// way), so matching on the error would relabel a single slow poll as
			// "timed out after <timeout>" even with the overall budget still alive.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, timedOut()
			}
			return nil, err
		}
		if reportTerminalStatuses[reportStatus(raw)] {
			return raw, nil
		}
		if !c.now().Before(deadline) {
			return nil, timedOut()
		}
		if err := c.sleep(ctx, interval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, timedOut()
			}
			return nil, &output.NetworkError{Err: err}
		}
	}
}

// reportStatus extracts the meaningful status nested at report.status from a raw
// report blob. The response wraps the blob under several sibling keys; the
// status the CLI needs for --wait lives at report.status, never at the top
// level. Returns "" when absent/unparseable, which the caller treats as
// non-terminal (keep polling).
func reportStatus(raw json.RawMessage) string {
	var envelope struct {
		Report struct {
			Status string `json:"status"`
		} `json:"report"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	return envelope.Report.Status
}

// sleepCtx is the default poll sleeper: it waits for d unless ctx is cancelled
// first, in which case it returns the context error.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
