---
name: trendhero-api
description: >-
  Query trendHERO Instagram influencer analytics — ranked top profiles and
  per-account audience/engagement reports — using the `th-cli` CLI. Use this
  whenever the user wants Instagram influencer data, top-account rankings by
  country/month, or an audience/engagement report for an Instagram username,
  even if they don't name trendHERO or the `th-cli` binary. Covers auth setup,
  fetching and ordering reports (ordering is paid), polling until a report is
  ready, and reading the JSON output and exit codes.
---

# trendHERO API (`th-cli` CLI)

`th-cli` is a thin wrapper around trendHERO's public API. It emits **JSON to
stdout** and encodes failure classes as **exit codes**, so you can branch on
results without scraping text. Two capabilities:

- **`top-profiles`** — ranked top Instagram accounts for a country/month
  (**no token required**).
- **`report`** — per-account audience/engagement reports (**token required**):
  `report get` fetches one, `report order` generates a new one (**paid**).

## Running `th-cli`

`th-cli` here is a small launcher bundled with this skill. On first use it downloads
and caches the real binary from public GitHub Releases — **no token is needed to
install it**. Invoke it by full path:

- Plugin install: `"${CLAUDE_PLUGIN_ROOT}/skills/trendhero-api/bin/th-cli"`
- Symlink install: `~/.claude/skills/trendhero-api/bin/th-cli`

For brevity the rest of this doc writes `th-cli` — substitute the launcher path (or
plain `th-cli` if the user has symlinked it onto `PATH`). The first call fetches the
platform binary; if it can't (offline, no `curl`/`wget`), it prints exactly what
to do and exits **69** — a provisioning error, distinct from the API exit codes
below. Relay that message to the user rather than guessing.

## Quickstart

1. **Confirm it works** — run the launcher: `th-cli version` prints
   `{"version","commit"}`. The first run downloads the binary; if it exits 69,
   relay its instructions to the user.
2. **Set a token for report commands** (`top-profiles` needs none). If a report
   command exits **2** ("no AccessToken configured"), walk the user through it:
   - Get a token at **https://trendhero.io/app/api/access-tokens** (needs the
     AdvancedApi subscription; one per Space).
   - Then store it. The **most reliable way for the skill** is the config file —
     a shell `export` may not reach the binary depending on how Claude was
     launched, but the file always does:
     ```yaml
     # ~/.config/th-cli/config.yaml
     token: <AccessToken>
     ```
     You may offer to create that file for the user once they share their token.
     Alternatively `export TRENDHERO_TOKEN=<token>` in the shell that starts
     Claude, or pass `--token <token>` per call. See `references/auth.md`.
3. **Read results from stdout as JSON; branch on the exit code** (table below).

## Command reference

```
th-cli version                          # build info as JSON
th-cli top-profiles [flags]             # ranked top accounts (no token)
th-cli report get <username> [flags]    # fetch a report (token required)
th-cli report order <username> [flags]  # order a NEW report — PAID (token required)
```

Global flags (any command): `--token`, `--base-url` (advanced host override),
`--config <path>`.

**`top-profiles`** flags: `--country` (one of `US UA RU DE FR TR BR IT PL`),
`--type absolute|relative` (default `absolute`; `relative` is not yet
implemented server-side → 422), `--year`, `--month` (1-12). All optional; the
server applies defaults. The token is sent only if configured, never required.

**`report get <username>`** flags: `--wait` (poll until the report reaches a
terminal status), `--timeout` (default `5m`), `--interval` (default `10s`).
Prefer `report get <username> --wait` so you get a usable report in one call
instead of polling yourself.

**`report order <username>`** flags: `--confirm` (**required** — see below),
`--wait`, `--timeout`, `--interval`.

## Reports: status and the order→wait→get workflow

A report's meaningful status is nested at **`report.status`** in the JSON (not a
top-level field). Statuses:

- `ready`, `recollecting`, `impossibleButReady` — usable, terminal.
- `impossible` — terminal but failed (account is private/too small/etc.).
- `collecting` — still generating; keep polling.

Typical flow: try `report get <username> --wait` first. If it 404s, the report
was never ordered — `report order <username> --confirm --wait` generates one and
waits for it. `--wait` stops at **any** terminal status (including `impossible`)
and **exits 0** — always read `report.status` from the JSON to know the outcome.
Details in `references/reports.md`.

## `report order` is paid and guarded

Ordering **spends account credits**. The command refuses unless you pass
`--confirm` (or set `TRENDHERO_ALLOW_WRITES=1`); without that opt-in it makes
**no** API call and exits 1. Only order when the user has clearly asked for a
new/fresh report — otherwise fetch the existing one with `report get`.

## Output and exit codes

Success → pretty JSON on **stdout**. Failure → `{"error":...,"hint":...}` on
**stderr** plus a non-zero exit code:

| Code | Meaning | Typical fix |
|------|---------|-------------|
| 0 | success | — |
| 1 | usage / generic (e.g. invalid `--country`, order without `--confirm`) | fix the command |
| 2 | auth — missing/invalid token | set `TRENDHERO_TOKEN` (`references/auth.md`) |
| 3 | forbidden (403) — subscription/feature/balance | check account access |
| 4 | not found (404) — no such report | order it first |
| 5 | network / timeout (incl. `--wait` timeout) | check connectivity / base URL |
| 6 | validation (422) — e.g. `not_enough_balance`, `relative` type | read `error` message |
| 7 | service unavailable (503) | retry shortly |

The 422 message is surfaced verbatim in the `error` field — read it rather than
guessing.

## References

Read these when you need detail beyond the above:

- `references/auth.md` — minting an AccessToken, AdvancedApi requirement, env
  vars, optional base-URL override.
- `references/reports.md` — status meanings, the nested `report.status`, the
  order→wait→get workflow, exit codes 6/7.
- `references/examples.md` — copy-paste recipes (top US profiles, fetch a
  report, order + wait).
