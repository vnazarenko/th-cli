---
name: th-cli
description: >-
  Query trendHERO Instagram influencer analytics — ranked top profiles,
  influencer discovery search, and per-account audience/engagement reports —
  using the `th-cli` CLI. Use this whenever the user wants Instagram influencer
  data: top-account rankings by country/month, an audience/engagement report for
  a username, or FINDING accounts that match criteria (niche/keywords, follower
  count, engagement rate, country, language, gender, verified, has-contacts, or
  audience demographics), even if they don't name trendHERO or the `th-cli`
  binary. Covers auth setup, searching (paid — spends credits per result),
  fetching and ordering reports (ordering is paid), polling until a report is
  ready, and reading the JSON output and exit codes.
---

# trendHERO API (`th-cli` CLI)

`th-cli` is a thin wrapper around trendHERO's public API. It emits **JSON to
stdout** and encodes failure classes as **exit codes**, so you can branch on
results without scraping text. Three capabilities:

- **`top-profiles`** — ranked top Instagram accounts for a country/month
  (**no token required**).
- **`search`** — find accounts matching a filter set: niche, follower count,
  engagement rate, country, language, gender, contacts, audience demographics
  (**token required**, **paid** — spends credits per result returned).
- **`report`** — per-account audience/engagement reports (**token required**):
  `report get` fetches one, `report order` generates a new one (**paid**).

Typical chain: `search` to find candidate usernames → `report get` for the ones
worth analysing.

## Running `th-cli`

`th-cli` here is a small launcher bundled with this skill. On first use it downloads
and caches the real binary from public GitHub Releases — **no token is needed to
install it**. Invoke it by full path:

- Plugin install: `"${CLAUDE_PLUGIN_ROOT}/skills/th-cli/bin/th-cli"`
- Symlink install: `~/.claude/skills/th-cli/bin/th-cli`

For brevity the rest of this doc writes `th-cli` — substitute the launcher path (or
plain `th-cli` if the user has symlinked it onto `PATH`). The first call fetches the
platform binary; if it can't (offline, no `curl`/`wget`), it prints exactly what
to do and exits **69** — a provisioning error, distinct from the API exit codes
below. Relay that message to the user rather than guessing.

## Quickstart

1. **Confirm it works** — run the launcher: `th-cli version` prints
   `{"version","commit"}`. The first run downloads the binary; if it exits 69,
   relay its instructions to the user.
2. **Set a token for `search` and the report commands** (`top-profiles` needs
   none). If one of them exits **2** ("no AccessToken configured"), walk the
   user through it:
   - Get a token at **https://trendhero.io/app/api/access-tokens** (needs the
     AdvancedApi subscription; one per Space).
   - Then store it. The **most reliable way for the skill** is the config file —
     a shell `export` may not reach the binary depending on how Claude was
     launched, but the file always does. A ready-to-edit template ships at
     `${CLAUDE_PLUGIN_ROOT}/config.example.yaml`; copy it and fill in the token:
     ```bash
     mkdir -p ~/.config/th-cli
     cp "${CLAUDE_PLUGIN_ROOT}/config.example.yaml" ~/.config/th-cli/config.yaml
     # then set `token:` in ~/.config/th-cli/config.yaml
     ```
     You may offer to do this for the user once they share their token.
     Alternatively `export TRENDHERO_TOKEN=<token>` in the shell that starts
     Claude, or pass `--token <token>` per call. See `references/auth.md`.
3. **Read results from stdout as JSON; branch on the exit code** (table below).

## Command reference

```
th-cli version                          # build info as JSON
th-cli top-profiles [flags]             # ranked top accounts (no token)
th-cli search [flags]                   # find accounts by filters — PAID (token required)
th-cli report get <username> [flags]    # fetch a report (token required)
th-cli report order <username> [flags]  # order a NEW report — PAID (token required)
```

Global flags (any command): `--token`, `--base-url` (advanced host override),
`--config <path>`.

**`top-profiles`** flags: `--country` (one of `US UA RU DE FR TR BR IT PL`),
`--type absolute|relative` (default `absolute`; `relative` is not yet
implemented server-side → 422), `--year`, `--month` (1-12). All optional; the
server applies defaults. The token is sent only if configured, never required.

**`search`** flags: `--keywords`, `--followers-min`, `--followers-max`,
`--er-min`, `--er-max` (engagement rate in percent), `--country`, `--language`,
`--category`, `--gender` (`male|female|none|brand`), `--verified`,
`--with-contacts` (`biography_contacts|trendhero_contacts`), plus
`--filters-json <file|->` for the full filter surface (premium audience
filters, `sort`, city locations, age brackets) merged **over** those flags, and
`--page` / `--size` for paging. See "`search` is PAID" below and
`references/search.md`.

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

## Two paid commands: `search` and `report order`

### `search` is PAID — and **not** guarded by `--confirm`

Every `search` call **spends credits**: a flat charge per search plus one **per
result returned**. There is no free preview and no count-only mode.
Deliberately, there is no `--confirm` prompt (a search is cheap per call and
agents run many, so a guard would just be clicked through) — which makes it your
job to be economical:

- **`--size` is a cost multiplier.** It defaults to 15 and caps at 50. Ask for
  what you need, not for 50 by reflex.
- **There is no `--all` / auto-paging flag**, on purpose: each page is a
  separate billed call. Only page further when the user's question needs it.
- **Don't re-run the same search** to "check something" — keep the JSON.

Three gotchas that cost round trips:

1. **Pages are 0-indexed.** `--page 0` is the first page (the default).
2. **`--size` must be 1–50.** Out of range is a hard error, **never clamped**.
3. **A zero-match search is a SUCCESS** — HTTP 200, exit 0, `"results": []`.
   Read `results`, not the exit code, to learn that nothing matched.

Full filter reference, worked examples and the error table:
`references/search.md`.

### `report order` is paid and guarded

Ordering **spends account credits**. The command refuses unless you pass
`--confirm` (or set `TRENDHERO_ALLOW_WRITES=1`); without that opt-in it makes
**no** API call and exits 1. Only order when the user has clearly asked for a
new/fresh report — otherwise fetch the existing one with `report get`.

A `search` hit carrying `"paid": true` means your Space already owns that
report — fetch it with `report get` instead of ordering it again.

## Output and exit codes

Success → pretty JSON on **stdout**. Failure → `{"error":...,"hint":...}` on
**stderr** plus a non-zero exit code:

| Code | Meaning | Typical fix |
|------|---------|-------------|
| 0 | success | — |
| 1 | usage / generic (e.g. invalid `--country`, bad `--size`, order without `--confirm`) | fix the command |
| 2 | auth — missing/invalid token | set `TRENDHERO_TOKEN` (`references/auth.md`) |
| 3 | forbidden (403) — subscription/feature, or a capability disabled by an admin | check account access |
| 4 | not found (404) — no such report | order it first |
| 5 | network / timeout (incl. `--wait` timeout) | check connectivity / base URL |
| 6 | validation (422) — e.g. `not_enough_balance`, a bad filter shape, a page past the plan cap, `relative` type | read `error` message |
| 7 | service unavailable (503) | retry shortly |

The 422 message is surfaced verbatim in the `error` field — read it rather than
guessing.

For `search`, exit 1 means the CLI rejected the command **before making any
call**, so nothing was charged; every 422 is unbilled except "not enough
balance". Only a successful search costs credits.

## References

Read these when you need detail beyond the above:

- `references/auth.md` — minting an AccessToken, AdvancedApi requirement, env
  vars, optional base-URL override.
- `references/search.md` — **read before running `search`**: the full 30-filter
  reference, `--filters-json` and its merge rule, the 0-indexed page and 1–50
  size gotchas, the plan result cap, premium filters, and what each error means.
- `references/reports.md` — status meanings, the nested `report.status`, the
  order→wait→get workflow, exit codes 6/7.
- `references/examples.md` — copy-paste recipes (top US profiles, fetch a
  report, order + wait).
